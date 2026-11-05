# syntax=docker/dockerfile:1

# 1. Frontend
FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2. Server Go con il frontend embeddato
FROM golang:1.26-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tunescout-web ./cmd/server

# 3. Immagine finale
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/tunescout-web /tunescout-web
ENV ADDR=:8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/tunescout-web"]
