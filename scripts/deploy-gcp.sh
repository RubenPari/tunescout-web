#!/usr/bin/env bash
# Deploy di tunescout-web su Google Cloud:
#   Cloud Run (app Go + frontend embeddato) · Cloud Build · Artifact Registry ·
#   Secret Manager · Cloud SQL Postgres (istanza esistente, anche in un altro progetto).
#
# Uso:  cp deploy.env.example deploy.env   # compila i valori
#       ./scripts/deploy-gcp.sh            # oppure: make deploy
#
# Lo script è idempotente: alla prima esecuzione crea tutto, dopo pubblica solo
# una nuova revisione. Non ruota mai TOKEN_ENC_KEY né la password del database.
#
# DEPLOY_ONLY=1 salta la configurazione (passi 2-8: progetto, API, IAM, database,
# segreti) e fa solo build, deploy e verifica: è la modalità usata dalla CI
# (.github/workflows/deploy.yml), con permessi minimi. Opzionali: DB_CONN
# (connection name di Cloud SQL, altrimenti viene letto) e IMAGE_TAG.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY_ENV="${DEPLOY_ENV:-$ROOT/deploy.env}"

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[!]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[x]\033[0m %s\n' "$*" >&2; exit 1; }

# Ripete un comando alcune volte: serve per la propagazione IAM dei service account nuovi.
retry() {
  local n=0
  until "$@"; do
    n=$((n + 1))
    [ "$n" -ge 6 ] && return 1
    warn "tentativo $n fallito, riprovo tra $((n * 5))s…"
    sleep $((n * 5))
  done
}

# ---------------------------------------------------------------------------
# 0. Configurazione
# ---------------------------------------------------------------------------
if [ -f "$DEPLOY_ENV" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$DEPLOY_ENV"
  set +a
else
  warn "$DEPLOY_ENV non trovato: uso solo le variabili d'ambiente"
fi

: "${GCP_PROJECT_ID:?GCP_PROJECT_ID non impostata (vedi deploy.env.example)}"
: "${DB_PROJECT:?DB_PROJECT non impostata}"
: "${DB_INSTANCE:?DB_INSTANCE non impostata}"
GCP_BILLING_ACCOUNT="${GCP_BILLING_ACCOUNT:-}"
REGION="${REGION:-europe-west8}"
SERVICE="${SERVICE:-tunescout-web}"
AR_REPO="${AR_REPO:-tunescout}"
DB_NAME="${DB_NAME:-tunescout}"
DB_USER="${DB_USER:-tunescout_app}"
MIN_INSTANCES="${MIN_INSTANCES:-0}"
MEMORY="${MEMORY:-512Mi}"
BASE_URL="${BASE_URL:-}"

export CLOUDSDK_CORE_DISABLE_PROMPTS=1
P=(--project "$GCP_PROJECT_ID")
DBP=(--project "$DB_PROJECT")

# ---------------------------------------------------------------------------
# 1. Controlli preliminari
# ---------------------------------------------------------------------------
command -v gcloud >/dev/null || die "gcloud non installato: https://cloud.google.com/sdk/docs/install"
command -v openssl >/dev/null || die "openssl non installato"
command -v curl >/dev/null || die "curl non installato"
ACCOUNT="$(gcloud auth list --filter=status:ACTIVE --format='value(account)' | head -n1)"
[ -n "$ACCOUNT" ] || die "nessun account gcloud attivo: esegui 'gcloud auth login'"
log "account: $ACCOUNT · progetto: $GCP_PROJECT_ID · regione: $REGION"

RUN_SA="tunescout-run@${GCP_PROJECT_ID}.iam.gserviceaccount.com"

# Passi 2-8: configurazione una tantum, saltati con DEPLOY_ONLY=1.
setup() {
# ---------------------------------------------------------------------------
# 2. Progetto e fatturazione
# ---------------------------------------------------------------------------
if ! gcloud projects describe "$GCP_PROJECT_ID" >/dev/null 2>&1; then
  log "creo il progetto $GCP_PROJECT_ID"
  gcloud projects create "$GCP_PROJECT_ID" --name="${GCP_PROJECT_NAME:-tunescout-web}"
fi
PROJECT_NUMBER="$(gcloud projects describe "$GCP_PROJECT_ID" --format='value(projectNumber)')"

if [ "$(gcloud billing projects describe "$GCP_PROJECT_ID" --format='value(billingEnabled)')" != "True" ]; then
  [ -n "$GCP_BILLING_ACCOUNT" ] || die "fatturazione non attiva: imposta GCP_BILLING_ACCOUNT (gcloud billing accounts list)"
  log "collego il billing account $GCP_BILLING_ACCOUNT"
  gcloud billing projects link "$GCP_PROJECT_ID" --billing-account="$GCP_BILLING_ACCOUNT"
fi

# ---------------------------------------------------------------------------
# 3. API
# ---------------------------------------------------------------------------
log "abilito le API necessarie"
gcloud services enable "${P[@]}" \
  run.googleapis.com \
  cloudbuild.googleapis.com \
  artifactregistry.googleapis.com \
  secretmanager.googleapis.com \
  sqladmin.googleapis.com \
  compute.googleapis.com \
  iam.googleapis.com

# ---------------------------------------------------------------------------
# 4. Artifact Registry
# ---------------------------------------------------------------------------
if ! gcloud artifacts repositories describe "$AR_REPO" --location "$REGION" "${P[@]}" >/dev/null 2>&1; then
  log "creo il repository Docker $AR_REPO"
  gcloud artifacts repositories create "$AR_REPO" --repository-format=docker \
    --location "$REGION" --description="Immagini tunescout-web" "${P[@]}"
fi

# ---------------------------------------------------------------------------
# 5. Service account e permessi
# ---------------------------------------------------------------------------
BUILD_SA="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"

if ! gcloud iam service-accounts describe "$RUN_SA" "${P[@]}" >/dev/null 2>&1; then
  log "creo il service account $RUN_SA"
  gcloud iam service-accounts create tunescout-run --display-name="tunescout-web (Cloud Run)" "${P[@]}"
fi

# Il service account di default di Compute viene creato in modo asincrono.
log "attendo il service account di Cloud Build ($BUILD_SA)"
retry gcloud iam service-accounts describe "$BUILD_SA" "${P[@]}" >/dev/null 2>&1 \
  || die "service account $BUILD_SA non disponibile"

bind() { # bind <progetto> <membro> <ruolo>
  retry gcloud projects add-iam-policy-binding "$1" --member="$2" --role="$3" \
    --condition=None --quiet >/dev/null
}
log "assegno i ruoli IAM"
bind "$GCP_PROJECT_ID" "serviceAccount:$RUN_SA" roles/secretmanager.secretAccessor
bind "$DB_PROJECT" "serviceAccount:$RUN_SA" roles/cloudsql.client
bind "$GCP_PROJECT_ID" "serviceAccount:$BUILD_SA" roles/cloudbuild.builds.builder

# ---------------------------------------------------------------------------
# 6. Secret Manager (helper)
# ---------------------------------------------------------------------------
secret_exists() { gcloud secrets describe "$1" "${P[@]}" >/dev/null 2>&1; }
secret_get()    { gcloud secrets versions access latest --secret "$1" "${P[@]}" 2>/dev/null; }

# secret_put <nome> <valore>: crea il secret o aggiunge una versione se il valore è cambiato.
secret_put() {
  local name="$1" value="$2"
  if ! secret_exists "$name"; then
    log "creo il secret $name"
    printf '%s' "$value" | gcloud secrets create "$name" --replication-policy=automatic \
      --data-file=- "${P[@]}" >/dev/null
  elif [ "$(secret_get "$name" || true)" != "$value" ]; then
    log "aggiorno il secret $name"
    printf '%s' "$value" | gcloud secrets versions add "$name" --data-file=- "${P[@]}" >/dev/null
  fi
}

# secret_from_env <nome>: usa la variabile omonima; se è vuota tiene il secret esistente.
secret_from_env() {
  local name="$1" value="${!1:-}"
  if [ -n "$value" ]; then
    secret_put "$name" "$value"
  elif ! secret_exists "$name"; then
    die "$name non impostata in $DEPLOY_ENV e secret assente: è obbligatoria al primo deploy"
  fi
}

# ---------------------------------------------------------------------------
# 7. Database sull'istanza Cloud SQL esistente
# ---------------------------------------------------------------------------
DB_CONN="$(gcloud sql instances describe "$DB_INSTANCE" "${DBP[@]}" --format='value(connectionName)')" \
  || die "istanza Cloud SQL $DB_PROJECT/$DB_INSTANCE non trovata"
log "database: $DB_NAME su $DB_CONN"

if ! gcloud sql databases describe "$DB_NAME" --instance "$DB_INSTANCE" "${DBP[@]}" >/dev/null 2>&1; then
  log "creo il database $DB_NAME"
  gcloud sql databases create "$DB_NAME" --instance "$DB_INSTANCE" "${DBP[@]}"
fi

user_exists=false
if gcloud sql users list --instance "$DB_INSTANCE" "${DBP[@]}" --format='value(name)' | grep -qx "$DB_USER"; then
  user_exists=true
fi

if ! secret_exists DATABASE_URL; then
  DB_PASSWORD="$(openssl rand -hex 24)" # hex: niente caratteri da codificare nell'URL
  if $user_exists; then
    warn "l'utente $DB_USER esiste ma manca il secret DATABASE_URL: reimposto la password"
    gcloud sql users set-password "$DB_USER" --instance "$DB_INSTANCE" --password "$DB_PASSWORD" "${DBP[@]}"
  else
    log "creo l'utente $DB_USER"
    gcloud sql users create "$DB_USER" --instance "$DB_INSTANCE" --password "$DB_PASSWORD" "${DBP[@]}"
  fi
  # Connessione tramite il socket del connettore Cloud SQL di Cloud Run (già cifrato).
  secret_put DATABASE_URL "postgres://${DB_USER}:${DB_PASSWORD}@/${DB_NAME}?host=/cloudsql/${DB_CONN}&sslmode=disable"
elif ! $user_exists; then
  die "il secret DATABASE_URL esiste ma l'utente $DB_USER no: elimina il secret (gcloud secrets delete DATABASE_URL --project $GCP_PROJECT_ID) e rilancia"
fi

# ---------------------------------------------------------------------------
# 8. Segreti dell'app
# ---------------------------------------------------------------------------
secret_from_env SPOTIFY_CLIENT_ID
secret_from_env SPOTIFY_CLIENT_SECRET
secret_from_env ANTHROPIC_API_KEY
if ! secret_exists TOKEN_ENC_KEY; then
  secret_put TOKEN_ENC_KEY "${TOKEN_ENC_KEY:-$(openssl rand -base64 32)}"
fi
} # fine setup

if [ "${DEPLOY_ONLY:-0}" = 1 ]; then
  log "DEPLOY_ONLY: salto la configurazione, eseguo solo build e deploy"
  DB_CONN="${DB_CONN:-$(gcloud sql instances describe "$DB_INSTANCE" "${DBP[@]}" --format='value(connectionName)')}" \
    || die "istanza Cloud SQL $DB_PROJECT/$DB_INSTANCE non trovata (oppure imposta DB_CONN)"
else
  setup
fi

# ---------------------------------------------------------------------------
# 9. Build dell'immagine con Cloud Build
# ---------------------------------------------------------------------------
TAG="$(date +%Y%m%d-%H%M%S)"
if [ -n "${IMAGE_TAG:-}" ]; then
  TAG="$IMAGE_TAG"
elif git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  TAG="$(git -C "$ROOT" rev-parse --short HEAD)"
  if [ -n "$(git -C "$ROOT" status --porcelain)" ]; then
    TAG="${TAG}-dirty-$(date +%H%M%S)"
  fi
fi
IMAGE="${REGION}-docker.pkg.dev/${GCP_PROJECT_ID}/${AR_REPO}/${SERVICE}:${TAG}"

log "build dell'immagine $IMAGE"
retry gcloud builds submit "$ROOT" --tag "$IMAGE" "${P[@]}"

# ---------------------------------------------------------------------------
# 10. Deploy su Cloud Run
# ---------------------------------------------------------------------------
if [ -z "$BASE_URL" ]; then
  PROJECT_NUMBER="${PROJECT_NUMBER:-$(gcloud projects describe "$GCP_PROJECT_ID" --format='value(projectNumber)')}"
  BASE_URL="https://${SERVICE}-${PROJECT_NUMBER}.${REGION}.run.app"
fi
BASE_URL="${BASE_URL%/}"

ENV_VARS="BASE_URL=${BASE_URL}"
for v in TUNESCOUT_MODEL TUNESCOUT_EFFORT DAILY_RUN_LIMIT MAX_CONCURRENT_JOBS; do
  if [ -n "${!v:-}" ]; then ENV_VARS="${ENV_VARS}@${v}=${!v}"; fi
done

SECRETS="DATABASE_URL=DATABASE_URL:latest"
for s in SPOTIFY_CLIENT_ID SPOTIFY_CLIENT_SECRET ANTHROPIC_API_KEY TOKEN_ENC_KEY; do
  SECRETS="${SECRETS},${s}=${s}:latest"
done

# Una sola istanza: lo stato live dei job è in memoria (vedi README).
# CPU sempre allocata: i job girano in background anche dopo la fine della richiesta.
log "deploy del servizio $SERVICE"
gcloud run deploy "$SERVICE" "${P[@]}" \
  --region "$REGION" \
  --image "$IMAGE" \
  --service-account "$RUN_SA" \
  --add-cloudsql-instances "$DB_CONN" \
  --set-env-vars "^@^${ENV_VARS}" \
  --set-secrets "$SECRETS" \
  --port 8080 \
  --memory "$MEMORY" \
  --cpu 1 \
  --min-instances "$MIN_INSTANCES" \
  --max-instances 1 \
  --no-cpu-throttling \
  --timeout 3600 \
  --allow-unauthenticated

URL="$(gcloud run services describe "$SERVICE" --region "$REGION" "${P[@]}" --format='value(status.url)')"

# ---------------------------------------------------------------------------
# 11. Verifica
# ---------------------------------------------------------------------------
log "controllo $URL/api/health"
if retry curl -fsS --max-time 20 "$URL/api/health" >/dev/null; then
  log "deploy completato ✔"
else
  die "healthcheck fallito: gcloud run services logs read $SERVICE --region $REGION --project $GCP_PROJECT_ID"
fi

cat <<EOF

  URL servizio : $URL
  BASE_URL     : $BASE_URL
  Redirect URI : ${BASE_URL}/api/auth/callback
                 (registralo nella dashboard Spotify, se non l'hai già fatto)

EOF
