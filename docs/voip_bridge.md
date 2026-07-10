# VoIP bridge — connect a WhatsApp call to a human operator (WebRTC)

Bridges a WhatsApp voice call (the media stack in `voip/`) to a human agent in
the browser, over a WebRTC data channel carrying raw PCM. Ported from the
reference `williamprado/WaCalls` (`cmd/server/bridge.go` + browser client).

> ⚠️ **VERY HIGH account-ban risk.** Disposable accounts only, never production.

## How it works

The call core only ever deals in `[]float32` PCM (16 kHz mono) via
`voip.Manager.FeedCapturedPCM` / `OnPeerAudio`. The bridge is a thin WebRTC leg
that carries that PCM to/from the browser:

```
Browser (operator mic) ──DataChannel "pcm"──► Bridge.OnBrowserPCM ──► Manager.FeedCapturedPCM ──► WhatsApp
WhatsApp (caller voice) ──► Manager.OnPeerAudio ──► Bridge.WritePCM ──DataChannel "pcm"──► Browser (speaker)
```

PCM is sent as **16 kHz mono Int16 LE** on a WebRTC ordered data channel labeled
`pcm`. No Opus/SRTP on the browser leg — that is all handled on the WhatsApp side
by the `voip/` media stack.

## Components

- **`voip/bridge`** — `bridge.Bridge`: answers the browser's SDP offer, sets up
  the `pcm` data channel, exposes `OnBrowserPCM` (mic in) and `WritePCM` (peer
  audio out). Depends only on `pion/webrtc/v4` and `voip/media` (PCM codecs).
- **`voip/examples/voip-bridge-server`** — a slim, single-session HTTP server:
  - logs in one WhatsApp account (QR on first run),
  - serves a self-contained agent page (`agent.html`, vanilla JS, inlined
    AudioWorklets — no build step),
  - REST + SSE control surface (below), wiring each call to a `Bridge`.

## HTTP surface (single session)

| Method + path | Action |
|---|---|
| `GET /` | agent page |
| `GET /metrics` | Prometheus metrics (calls started/answered/ended, active, setup/duration histograms) |
| `GET /api/config` | ICE servers (STUN/TURN, ephemeral TURN creds) for the browser |
| `GET /api/events` | SSE: `incoming` / `state` / `ended` |
| `POST /api/call` `{to}` | place an outbound call → `{call_id}` |
| `POST /api/call/{id}/webrtc` `{sdp_offer}` | attach the browser leg → `{sdp_answer}` |
| `POST /api/call/{id}/accept` | answer an inbound call |
| `POST /api/call/{id}/reject` | decline an inbound call |
| `DELETE /api/call/{id}` | hang up |

### Auth & ICE (P0 hardening)

The demo now carries a **bearer-token auth skeleton** and **STUN/TURN config**
(see `docs/voip_production.md` P0). Both are demo-grade — production wires the
same ideas into the authenticated AtendZappy backend.

- **Auth:** set `AUTH_TOKEN` to require `Authorization: Bearer <token>` on every
  `/api/*` call. The SSE stream takes it as `?token=` (EventSource can't set
  headers). The agent page reads the token from `?token=` or `localStorage`. When
  `AUTH_TOKEN` is unset the surface is **open** (dev only) and the server warns.
- **ICE:** `GET /api/config` returns the ICE servers; the browser uses them and
  the server-side pion leg uses the matching set. With `TURN_SECRET`, fresh
  **ephemeral TURN credentials** are minted per call (coturn REST API:
  `username = <expiryUnix>`, `credential = base64(HMAC-SHA1(secret, username))`;
  run coturn with `use-auth-secret` + `static-auth-secret`).

## Run (disposable accounts only)

```sh
cd voip
VOIP_ENABLED=1 go run ./examples/voip-bridge-server
# scan the QR (first run) with a DISPOSABLE account, then open http://localhost:8080

# with auth + STUN/TURN (production-shaped):
VOIP_ENABLED=1 AUTH_TOKEN=secret \
  STUN_URLS=stun:stun.l.google.com:19302 \
  TURN_URLS=turn:turn.example.com:3478 TURN_SECRET=<coturn-shared-secret> \
  go run ./examples/voip-bridge-server
# open http://localhost:8080/?token=secret
```

Env: `VOIP_ENABLED` (`1` to place/answer), `ADDR` (default `:8080`), `SESSION_DB`,
`AUTH_TOKEN`, `STUN_URLS`, `TURN_URLS`, `TURN_SECRET` (or `TURN_USER`/`TURN_PASS`),
`TURN_TTL` (ephemeral cred seconds, default 3600), `DATABASE_URL` (Postgres session
store + CDR; falls back to SQLite when unset), `CDR_FILE` (JSON-lines CDR path when
not using Postgres, default `cdr.jsonl`).

### Store, metrics & CDR (P0 hardening)

- **Session store:** set `DATABASE_URL` to use **Postgres** (`sqlstore` dialect
  `postgres`) for multi-node / HA; unset falls back to the local **SQLite** file.
- **Metrics:** `GET /metrics` exposes Prometheus collectors from the reusable
  `voip/metrics` package (calls started by direction, answered, ended by reason,
  active gauge, setup/duration histograms, peer-audio frames). Put it on an
  internal port or behind scraper auth in production.
- **CDR:** the reusable `voip/cdr` package records one row per call (direction,
  peer, timestamps, setup/duration, end reason). Sink is **Postgres** (`voip_cdr`
  table, auto-created) when `DATABASE_URL` is set, else **JSON lines** to `CDR_FILE`.

- **Outbound:** type a number (digits only) → *Ligar*. The browser asks for mic
  permission, connects the WebRTC leg, and you talk once the callee answers.
- **Inbound:** an incoming call shows a banner → *Atender* / *Rejeitar*.

## Status & validation

- `go build/vet/test ./...` green for the `voip/` module. `gofmt`/`goimports` clean.
- Smoke-tested: server boots, logs into a disposable account, serves the agent
  page (HTTP 200), SSE connects; **auth enforced** (`/api/config` → 401 without a
  token, 200 with it) and `/api/config` returns STUN + an ephemeral TURN cred.
- **Field-tested:** operator in the browser held a real WhatsApp call with
  **bidirectional audio** (both directions confirmed audibly).
- **Still pending for production:** TURN behind real NATs, relay reconnect,
  multiple concurrent operators, and integration into the authenticated backend.

## Notes

- This lives in the fork as a **companion example**, not in AtendZappy. A
  production deployment would put auth in front of the HTTP surface and likely
  drive the same `voip.Manager` from the existing engine rather than this demo
  server.
- No STUN/TURN is configured (`iceServers: []`) — fine for localhost/LAN; a real
  deployment needs ICE servers for the browser leg across NATs.
