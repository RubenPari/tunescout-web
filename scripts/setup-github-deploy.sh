#!/usr/bin/env bash
# Prepara il deploy automatico da GitHub Actions (.github/workflows/deploy.yml).
# Da eseguire una volta, dopo il primo `make deploy`, con un account gcloud
# proprietario del progetto e `gh` autenticato sul repository.
#
# Crea (in modo idempotente):
#   - il service account github-deploy con i soli permessi per build e deploy;
#   - un Workload Identity Pool/Provider per i token OIDC di GitHub, accettati
#     solo da questo repository e solo dai job nell'ambiente "production"
#     (niente chiavi JSON da conservare);
#   - le variabili del repository lette dal workflow.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY_ENV="${DEPLOY_ENV:-$ROOT/deploy.env}"
if [ -f "$DEPLOY_ENV" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$DEPLOY_ENV"
  set +a
fi

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m[x]\033[0m %s\n' "$*" >&2; exit 1; }

: "${GCP_PROJECT_ID:?GCP_PROJECT_ID non impostata (vedi deploy.env.example)}"
: "${DB_PROJECT:?DB_PROJECT non impostata}"
: "${DB_INSTANCE:?DB_INSTANCE non impostata}"
REGION="${REGION:-europe-west8}"
SERVICE="${SERVICE:-tunescout-web}"
REPO="${GITHUB_REPO:-$(gh repo view --json nameWithOwner --jq .nameWithOwner)}"
POOL=github
PROVIDER=github-oidc
GH_ENV=production

export CLOUDSDK_CORE_DISABLE_PROMPTS=1
P=(--project "$GCP_PROJECT_ID")
PROJECT_NUMBER="$(gcloud projects describe "$GCP_PROJECT_ID" --format='value(projectNumber)')"
DEPLOY_SA="github-deploy@${GCP_PROJECT_ID}.iam.gserviceaccount.com"
RUN_SA="tunescout-run@${GCP_PROJECT_ID}.iam.gserviceaccount.com"
BUILD_SA="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"
DB_CONN="$(gcloud sql instances describe "$DB_INSTANCE" --project "$DB_PROJECT" --format='value(connectionName)')"
log "repository: $REPO · progetto: $GCP_PROJECT_ID"

# --- service account di deploy ---------------------------------------------
if ! gcloud iam service-accounts describe "$DEPLOY_SA" "${P[@]}" >/dev/null 2>&1; then
  log "creo il service account $DEPLOY_SA"
  gcloud iam service-accounts create github-deploy --display-name="GitHub Actions deploy" "${P[@]}"
  sleep 10 # propagazione IAM
fi

log "assegno i ruoli al service account di deploy"
# viewer: necessario a `gcloud builds submit` per seguire i log della build.
for role in roles/run.admin roles/cloudbuild.builds.editor roles/viewer; do
  gcloud projects add-iam-policy-binding "$GCP_PROJECT_ID" --member="serviceAccount:$DEPLOY_SA" \
    --role="$role" --condition=None --quiet >/dev/null
done
# Caricamento del sorgente nel bucket di Cloud Build.
gcloud storage buckets add-iam-policy-binding "gs://${GCP_PROJECT_ID}_cloudbuild" \
  --member="serviceAccount:$DEPLOY_SA" --role=roles/storage.admin >/dev/null
# Agire come il service account di Cloud Run e quello delle build.
for sa in "$RUN_SA" "$BUILD_SA"; do
  gcloud iam service-accounts add-iam-policy-binding "$sa" "${P[@]}" \
    --member="serviceAccount:$DEPLOY_SA" --role=roles/iam.serviceAccountUser >/dev/null
done

# --- Workload Identity Federation ------------------------------------------
gcloud services enable iamcredentials.googleapis.com sts.googleapis.com "${P[@]}"

if ! gcloud iam workload-identity-pools describe "$POOL" --location=global "${P[@]}" >/dev/null 2>&1; then
  log "creo il Workload Identity Pool $POOL"
  gcloud iam workload-identity-pools create "$POOL" --location=global \
    --display-name="GitHub Actions" "${P[@]}"
fi

CONDITION="assertion.repository == '${REPO}' && assertion.environment == '${GH_ENV}'"
if ! gcloud iam workload-identity-pools providers describe "$PROVIDER" --location=global \
  --workload-identity-pool="$POOL" "${P[@]}" >/dev/null 2>&1; then
  log "creo il provider OIDC $PROVIDER"
  gcloud iam workload-identity-pools providers create-oidc "$PROVIDER" --location=global \
    --workload-identity-pool="$POOL" "${P[@]}" \
    --display-name="GitHub OIDC" \
    --issuer-uri="https://token.actions.githubusercontent.com" \
    --attribute-mapping="google.subject=assertion.sub,attribute.repository=assertion.repository,attribute.environment=assertion.environment" \
    --attribute-condition="$CONDITION"
else
  gcloud iam workload-identity-pools providers update-oidc "$PROVIDER" --location=global \
    --workload-identity-pool="$POOL" "${P[@]}" --attribute-condition="$CONDITION" >/dev/null
fi

POOL_ID="projects/${PROJECT_NUMBER}/locations/global/workloadIdentityPools/${POOL}"
gcloud iam service-accounts add-iam-policy-binding "$DEPLOY_SA" "${P[@]}" \
  --role=roles/iam.workloadIdentityUser \
  --member="principalSet://iam.googleapis.com/${POOL_ID}/attribute.repository/${REPO}" >/dev/null

# --- variabili del repository GitHub -----------------------------------------
log "imposto le variabili del repository $REPO"
setvar() { gh variable set "$1" --repo "$REPO" --body "$2"; }
setvar GCP_WORKLOAD_IDENTITY_PROVIDER "${POOL_ID}/providers/${PROVIDER}"
setvar GCP_DEPLOY_SERVICE_ACCOUNT "$DEPLOY_SA"
setvar GCP_PROJECT_ID "$GCP_PROJECT_ID"
setvar GCP_REGION "$REGION"
setvar DB_PROJECT "$DB_PROJECT"
setvar DB_INSTANCE "$DB_INSTANCE"
setvar DB_CONN "$DB_CONN"
# Opzionali: copiati solo se impostati in deploy.env.
for v in BASE_URL SERVICE MIN_INSTANCES MEMORY TUNESCOUT_MODEL TUNESCOUT_EFFORT DAILY_RUN_LIMIT MAX_CONCURRENT_JOBS; do
  if [ -n "${!v:-}" ]; then setvar "$v" "${!v}"; fi
done

log "fatto: ogni release pubblicata su GitHub avvia il deploy (ambiente \"$GH_ENV\")"
