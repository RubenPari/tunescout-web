.PHONY: build web test lint dev-server dev-web deploy

build: web
	go build -o tunescout-web ./cmd/server

web:
	cd web && npm ci && npm run build

test:
	go vet ./...
	go test ./...

lint:
	go vet ./...
	cd web && npm run lint

# Sviluppo: in due terminali, `make dev-server` e `make dev-web`.
# Il server legge le variabili da .env (con BASE_URL=http://127.0.0.1:5173).
dev-server:
	set -a && . ./.env && set +a && go run ./cmd/server

dev-web:
	cd web && npm run dev

# Deploy su Google Cloud (configurazione in deploy.env)
deploy:
	./scripts/deploy-gcp.sh
