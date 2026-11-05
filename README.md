# tunescout-web

[![CI](https://github.com/RubenPari/tunescout-web/actions/workflows/ci.yml/badge.svg)](https://github.com/RubenPari/tunescout-web/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Versione web multi-utente di [tunescout](https://github.com/RubenPari/tunescout): **scopri musica nuova**
partendo dalla tua libreria Spotify e **fai pulizia** dei brani salvati che non ascolti più.
I suggerimenti sono generati da **Claude** e verificati uno per uno su Spotify.

Backend in **Go** (stessa logica della CLI) e frontend **React + TypeScript**, distribuiti come un unico binario
o un'unica immagine Docker.

---

## Indice

- [Funzionalità](#funzionalità)
- [Architettura](#architettura)
- [Requisiti](#requisiti)
- [Configurazione](#configurazione)
- [Avvio con Docker](#avvio-con-docker)
- [Sviluppo in locale](#sviluppo-in-locale)
- [Deploy in produzione](#deploy-in-produzione)
- [API](#api)
- [Sicurezza e privacy](#sicurezza-e-privacy)
- [Risoluzione problemi](#risoluzione-problemi)
- [Struttura del progetto](#struttura-del-progetto)
- [Licenza](#licenza)

## Funzionalità

- **Accesso con Spotify** (OAuth): ogni utente vede solo i propri dati.
- **Libreria**: sincronizza brani salvati, top artisti e brani (4 settimane, 6 mesi, 1 anno) e playlist tue.
  La sincronizzazione parte da sola prima di Scopri e Fai pulizia se l'ultima ha più di 24 ore.
- **Scopri**: fino a 50 brani nuovi, con mood, generi e l'opzione "solo artisti che non ho in libreria".
  Ogni proposta viene cercata su Spotify. Vengono scartati i brani inesistenti, quelli che hai già, quelli già
  suggeriti e quelli che hai rimosso. Per ogni brano puoi mettere *Mi piace* (lo salva nella libreria), *Scarta*
  o aprirlo in Spotify. In più puoi salvare tutti i brani o creare una playlist privata.
- **Fai pulizia**: trova i brani salvati poco ascoltati e lontani dai tuoi gusti. Puoi creare una playlist di
  backup e rimuovere i brani selezionati, sempre dopo una conferma.
- **Storico**: tutti i suggerimenti ricevuti, con filtro per feedback. Like e scarti servono come segnale per i
  suggerimenti successivi.
- **Progresso in tempo reale**: le operazioni lunghe girano in background e il loro avanzamento arriva al
  browser via Server-Sent Events.
- **Limite giornaliero** configurabile per utente, per tenere sotto controllo i costi dell'API di Claude.

### Come funziona "Fai pulizia"

Spotify non espone il numero di ascolti. Per questo un brano è considerato "poco ascoltato" se:

- è salvato da almeno N giorni (90 di default);
- non è tra i tuoi top brani delle ultime 4 settimane / 6 mesi / 1 anno;
- nessuno dei suoi artisti è tra i 50 che ascolti di più.

Tra questi brani, Claude sceglie quelli meno in linea con il tuo profilo.

## Architettura

```
Browser (React SPA)
   │  cookie di sessione HttpOnly, JSON, SSE
   ▼
Server Go (net/http) ─── job in background (sync / discover / prune)
   │        │                      │
   │        ├── Spotify Web API ◄──┤  (token per utente, cifrati)
   │        └── Claude API ◄───────┘
   ▼
Postgres (dati separati per utente)
```

- Le pipeline `discover`, `prune` e `profile` sono le stesse della CLI. Cambia solo lo storage, che passa da
  SQLite locale a Postgres con `user_id` su ogni tabella.
- Il frontend viene compilato in `web/dist` e incluso nel binario con `embed`. Il server risponde su `/api/*`
  e serve la SPA per tutti gli altri percorsi.
- Ogni utente può avere al massimo un job alla volta. Il numero di job contemporanei sul server è limitato da
  `MAX_CONCURRENT_JOBS`.

## Requisiti

- Un'app Spotify (vedi sotto) e una API key Anthropic ([platform.claude.com](https://platform.claude.com))
- **Docker** con Docker Compose, *oppure* Go 1.26+, Node 24+ e Postgres 14+

## Configurazione

### 1. Crea l'app Spotify

1. Vai su <https://developer.spotify.com/dashboard> e crea una nuova app.
2. **Redirect URI**: `BASE_URL/api/auth/callback`, ad esempio
   - `http://127.0.0.1:8080/api/auth/callback` (Docker in locale)
   - `http://127.0.0.1:5173/api/auth/callback` (sviluppo con Vite)
   - `https://tuodominio.it/api/auth/callback` (produzione)
3. **API**: seleziona *Web API*.
4. Copia **Client ID** e **Client Secret**.
5. In **User Management** aggiungi gli account Spotify che useranno l'app (vedi la
   [nota sui limiti](#limiti-di-spotify)).

> Spotify accetta `http` solo per indirizzi di loopback come `127.0.0.1`, non per `localhost`.
> Per tutto il resto serve `https`.

### 2. Variabili d'ambiente

Copia `.env.example` in `.env` e compilalo:

| Variabile               | Obbligatoria | Descrizione |
|-------------------------|:-:|-------------|
| `BASE_URL`              | ✓ | URL pubblico dell'app, senza `/` finale. Determina il redirect OAuth e l'origine accettata per le richieste. |
| `DATABASE_URL`          | ✓ | Connessione Postgres. Con Docker Compose la imposta il compose. |
| `SPOTIFY_CLIENT_ID`     | ✓ | Client ID dell'app Spotify |
| `SPOTIFY_CLIENT_SECRET` | ✓ | Client Secret dell'app Spotify |
| `ANTHROPIC_API_KEY`     | ✓ | API key di Claude |
| `TOKEN_ENC_KEY`         | ✓ | 32 byte in base64 per cifrare i token Spotify: `openssl rand -base64 32` |
| `TUNESCOUT_MODEL`       |   | Modello Claude (default `claude-opus-5-5`) |
| `TUNESCOUT_EFFORT`      |   | `low` \| `medium` \| `high` \| `xhigh` \| `max` (default `medium`) |
| `DAILY_RUN_LIMIT`       |   | Esecuzioni di Scopri e Fai pulizia per utente al giorno, in UTC (default `20`, `0` = nessun limite). La sincronizzazione non conta. |
| `MAX_CONCURRENT_JOBS`   |   | Job contemporanei sul server (default `4`) |
| `ADDR`                  |   | Indirizzo di ascolto (default `:8080`) |

Le migrazioni del database vengono applicate in automatico all'avvio.

## Avvio con Docker

```bash
cp .env.example .env      # compila le variabili
docker compose up --build
```

Apri <http://127.0.0.1:8080> e accedi con Spotify.

## Sviluppo in locale

Serve un Postgres raggiungibile, ad esempio `docker compose up db`.

```bash
cp .env.example .env
# nel .env: BASE_URL=http://127.0.0.1:5173 e DATABASE_URL verso il tuo Postgres
# su Spotify registra il redirect http://127.0.0.1:5173/api/auth/callback

make dev-server   # terminale 1: API Go su :8080
make dev-web      # terminale 2: Vite su :5173, inoltra /api al server Go
```

Apri <http://127.0.0.1:5173>. Altri comandi:

```bash
make test    # go vet + test Go (avviano un Postgres embedded, nessuna configurazione)
make lint    # go vet + oxlint
make build   # build del frontend e binario ./tunescout-web con la SPA inclusa
```

I test Go scaricano al primo avvio un Postgres embedded. Per usare un server esistente imposta
`TEST_DATABASE_URL` (l'utente deve poter creare database).

## Deploy in produzione

1. Costruisci l'immagine: `docker build -t tunescout-web .` (distroless, utente non root).
2. Mettila dietro un reverse proxy con **HTTPS**. Con `BASE_URL` in `https://` i cookie diventano `Secure`.
3. Fornisci un Postgres gestito tramite `DATABASE_URL` e imposta le altre variabili come segreti.
4. Healthcheck: `GET /api/health` (controlla anche il database).
5. Lo stream SSE (`/api/jobs/{id}/events`) è long-lived. Il server invia già `X-Accel-Buffering: no` per nginx;
   se il proxy ha un timeout di lettura breve, alzalo ad almeno 60 secondi.

Note:

- **Una sola istanza**: lo stato live dei job (log in tempo reale) è in memoria. Con più istanze lo stato finale
  resta leggibile da Postgres, ma il progresso in diretta funziona solo sull'istanza che esegue il job.
- I job interrotti da un riavvio vengono segnati come falliti all'avvio successivo.
- Il rate limit sulle rotte di login usa l'IP della connessione. Dietro un proxy tutte le richieste arrivano
  dallo stesso IP, quindi conviene applicare un rate limit anche sul proxy.

### Deploy su Google Cloud

`scripts/deploy-gcp.sh` porta l'app su **Cloud Run**. L'immagine si costruisce con **Cloud Build** e va in
**Artifact Registry**, i segreti stanno in **Secret Manager** e il database è un'istanza **Cloud SQL Postgres**
già esistente, anche se si trova in un altro progetto. Docker in locale non serve.

```bash
gcloud auth login
cp deploy.env.example deploy.env   # progetto, billing, istanza Cloud SQL, chiavi Spotify/Anthropic
make deploy                        # oppure ./scripts/deploy-gcp.sh
```

Alla prima esecuzione lo script crea il progetto, lo collega al billing, abilita le API, crea il repository
Docker e il service account `tunescout-run`. Gli assegna `cloudsql.client` sul progetto del DB, crea database e
utente sull'istanza Cloud SQL (con password casuale) e salva i segreti. `TOKEN_ENC_KEY` viene generata in
automatico. Le esecuzioni successive fanno solo build e deploy di una nuova revisione. I segreti si aggiornano
solo se in `deploy.env` c'è un valore diverso.

Il servizio gira con `--max-instances 1` (vedi la nota sopra) e `--no-cpu-throttling`, così i job continuano in
background. Il timeout di 3600 s tiene aperto lo stream SSE. Alla fine lo script stampa l'URL e il **redirect URI
da registrare su Spotify**. Per un dominio personalizzato imposta `BASE_URL` in `deploy.env`.

Se l'avvio fallisce per i permessi sullo schema `public`, concedili una volta:
`gcloud sql connect <istanza> --user=postgres --database=tunescout --project <progetto-db>` e poi
`GRANT ALL ON SCHEMA public TO tunescout_app;`.

### Limiti di Spotify

Le app Spotify nuove sono in **Development Mode**: al massimo **25 utenti**, che vanno aggiunti a mano in
*User Management*. Gli altri account ricevono un errore 403 al login. Per aprire l'app a tutti bisogna chiedere
l'**Extended Quota Mode** a Spotify, che ha requisiti specifici. Spotify inoltre non offre più alle nuove app gli
endpoint `recommendations`, `related-artists` e `audio-features`: per questo i suggerimenti vengono da Claude e
sono poi verificati con la ricerca.

## API

Tutte le rotte, tranne health e auth, richiedono il cookie di sessione. Le richieste che modificano dati devono
provenire da `BASE_URL`: le richieste cross-origin vengono rifiutate con 403.

| Metodo | Percorso | Descrizione |
|--------|----------|-------------|
| `GET`  | `/api/health` | Stato del server e del database |
| `GET`  | `/api/auth/login` | Avvia il login Spotify |
| `GET`  | `/api/auth/callback` | Callback OAuth |
| `POST` | `/api/auth/logout` | Termina la sessione |
| `GET`  | `/api/me` | Utente, stato della libreria, utilizzo giornaliero, job attivo |
| `DELETE` | `/api/me` | Elimina l'account e tutti i dati dal server |
| `POST` | `/api/jobs/sync` | Avvia la sincronizzazione → `202 {id, …}` |
| `POST` | `/api/jobs/discover` | `{count, mood, genres[], newArtists}` → `202` |
| `POST` | `/api/jobs/prune` | `{count, minAgeDays}` → `202` |
| `GET`  | `/api/jobs/{id}` | Stato e risultato di un job |
| `GET`  | `/api/jobs/{id}/events` | SSE: eventi `log` e infine `done` con il job completo |
| `POST` | `/api/jobs/{id}/remove` | `{uris[]}`: rimuove dalla libreria brani proposti da quel prune |
| `POST` | `/api/feedback` | `{keys[], feedback: "like"\|"skip"}`; `like` salva anche nella libreria |
| `POST` | `/api/playlists` | `{name, uris[], description?}`: crea una playlist privata |
| `GET`  | `/api/history` | `?feedback=like\|skip&limit=N` |

Codici di errore principali: `401` (accesso richiesto; con `"reauth": true` il token Spotify va rinnovato),
`409` (operazione già in corso), `429` (limite giornaliero raggiunto o troppe richieste).

## Sicurezza e privacy

- **Token Spotify** cifrati con AES-256-GCM (`TOKEN_ENC_KEY`) e legati all'id utente: un token copiato su un
  altro utente non si decifra.
- **Sessioni**: id casuale a 256 bit in un cookie `HttpOnly` e `SameSite=Lax` (`Secure` in HTTPS). Nel database
  si salva solo il suo SHA-256. Le sessioni durano 30 giorni.
- **CSRF**: `http.CrossOriginProtection` di Go più `SameSite=Lax`. OAuth usa uno `state` casuale verificato.
- **Isolamento**: ogni query sui dati musicali filtra per `user_id`. Job e rimozioni sono verificati
  sull'utente proprietario. La rimozione accetta solo brani proposti dal prune indicato.
- **Header**: CSP restrittiva, `X-Frame-Options: DENY`, `nosniff`.
- **Dati inviati a terzi**: la libreria passa solo tra il server e Spotify. A Claude arriva un profilo di gusto
  (artisti, generi, alcuni brani) per generare i suggerimenti.
- **Cancellazione**: "Elimina il mio account" rimuove dal server utente, token, sessioni, cache, storico e job.
  La libreria Spotify non viene toccata.

## Risoluzione problemi

| Problema | Soluzione |
|----------|-----------|
| Errore 403 o "User Management" al login | Aggiungi l'account in *User Management* dell'app Spotify (Development Mode). |
| `INVALID_CLIENT: Invalid redirect URI` | Il redirect registrato deve essere esattamente `BASE_URL/api/auth/callback`. |
| "Sessione di login scaduta" | Il cookie di state dura 10 minuti, oppure `BASE_URL` non corrisponde al dominio che stai usando: riprova da `BASE_URL`. |
| "richiesta cross-origin rifiutata" | Stai aprendo l'app da un'origine diversa da `BASE_URL` (ad esempio `localhost` invece di `127.0.0.1`). |
| "autorizzazione Spotify scaduta o revocata" | Hai revocato l'accesso da Spotify oppure è cambiata `TOKEN_ENC_KEY`: accedi di nuovo. |
| "limite di N ricerche al giorno" | Aumenta `DAILY_RUN_LIMIT` oppure aspetta il giorno dopo (UTC). |
| `Anthropic: credenziali non valide` | Controlla `ANTHROPIC_API_KEY`. |
| "risposta troncata (max_tokens)" | Chiedi meno brani. |
| Pochi suggerimenti | Alcuni non esistono su Spotify o li conosci già: chiedi più brani o allenta i filtri. |
| Progresso fermo dietro un proxy | Disattiva il buffering per `/api/jobs/*/events` e alza il timeout di lettura. |

## Struttura del progetto

```
.
├── cmd/server/          # main: config, DB, router, shutdown ordinato
├── internal/
│   ├── api/             # handler HTTP, job, azioni, middleware (con test end-to-end)
│   ├── auth/            # OAuth Spotify, sessioni, cifratura dei token
│   ├── config/          # configurazione da variabili d'ambiente
│   ├── jobs/            # runner dei job in background e diffusione SSE
│   ├── store/           # Postgres: migrazioni, dati per utente, account, job
│   │   └── storetest/   # Postgres embedded per i test
│   ├── spotify/         # client Web API
│   ├── llm/             # integrazione con Claude
│   ├── profile/         # sincronizzazione libreria e profilo di gusto
│   ├── discover/        # pipeline: suggerimenti → verifica su Spotify → filtri
│   ├── prune/           # selezione dei brani poco ascoltati
│   └── norm/            # normalizzazione di titoli e artisti
├── web/                 # React 19 + Vite + TypeScript + Tailwind + TanStack Query
│   └── src/{pages,components}
├── Dockerfile, docker-compose.yml, Makefile, .env.example
```

## Licenza

Distribuito con licenza [MIT](LICENSE).
