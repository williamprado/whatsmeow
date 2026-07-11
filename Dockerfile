# syntax=docker/dockerfile:1
# ============================================================================
# whatsmeow (fork williamprado) — imagem da API VoIP (voip-bridge-server)
#
# O binário é o servidor HTTP de voip/examples/voip-bridge-server: página do
# operador embutida (go:embed), REST + SSE, WebRTC bridge, métricas Prometheus.
# O contexto de build precisa ser a RAIZ do repo: o módulo voip/ consome o
# whatsmeow deste checkout via "replace go.mau.fi/whatsmeow => ../".
#
# ⚠️ VoIP tem ALTÍSSIMO risco de ban: use apenas contas descartáveis e mantenha
# VOIP_ENABLED fora de produção até o sign-off (docs/voip_production.md §2).
# ============================================================================

FROM golang:1.26-alpine AS build

WORKDIR /src

# Cache de dependências: os go.mod/go.sum dos dois módulos mudam pouco.
COPY go.mod go.sum ./
COPY voip/go.mod voip/go.sum ./voip/
RUN cd voip && go mod download

COPY . .
RUN cd voip && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/voip-bridge-server ./examples/voip-bridge-server

# ----------------------------------------------------------------------------

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 app \
    && mkdir -p /data \
    && chown app:app /data

COPY --from=build /out/voip-bridge-server /usr/local/bin/voip-bridge-server

USER app
WORKDIR /data
VOLUME /data

# Sessão (SQLite) e CDR ficam no volume; com DATABASE_URL (Postgres) o SQLite
# não é usado. Demais envs: AUTH_TOKEN, STUN_URLS, TURN_URLS, TURN_SECRET,
# GUARD_* e VOIP_ENABLED — ver docs/voip_bridge.md.
ENV ADDR=:8080 \
    SESSION_DB=/data/session.db \
    CDR_FILE=/data/cdr.jsonl

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/voip-bridge-server"]
