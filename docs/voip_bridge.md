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
| `GET /api/events` | SSE: `incoming` / `state` / `ended` |
| `POST /api/call` `{to}` | place an outbound call → `{call_id}` |
| `POST /api/call/{id}/webrtc` `{sdp_offer}` | attach the browser leg → `{sdp_answer}` |
| `POST /api/call/{id}/accept` | answer an inbound call |
| `POST /api/call/{id}/reject` | decline an inbound call |
| `DELETE /api/call/{id}` | hang up |

## Run (disposable accounts only)

```sh
cd voip
VOIP_ENABLED=1 go run ./examples/voip-bridge-server
# scan the QR (first run) with a DISPOSABLE account, then open http://localhost:8080
```

Env: `VOIP_ENABLED` (`1` to place/answer), `ADDR` (default `:8080`), `SESSION_DB`.

- **Outbound:** type a number (digits only) → *Ligar*. The browser asks for mic
  permission, connects the WebRTC leg, and you talk once the callee answers.
- **Inbound:** an incoming call shows a banner → *Atender* / *Rejeitar*.

## Status & validation

- `go build/vet/test ./...` green for the `voip/` module (bridge + server build;
  media/mlow/signaling/transport suites pass). `gofmt`/`goimports` clean.
- Smoke-tested: server boots, logs into a disposable account, serves the agent
  page (HTTP 200), and the SSE stream connects.
- **Pending:** live browser↔phone audio field test (operator in the browser
  talking on a real WhatsApp call), and hardening (relay reconnect, multiple
  concurrent operators, auth on the HTTP surface) before any production use.

## Notes

- This lives in the fork as a **companion example**, not in AtendZappy. A
  production deployment would put auth in front of the HTTP surface and likely
  drive the same `voip.Manager` from the existing engine rather than this demo
  server.
- No STUN/TURN is configured (`iceServers: []`) — fine for localhost/LAN; a real
  deployment needs ICE servers for the browser leg across NATs.
