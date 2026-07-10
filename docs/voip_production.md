# VoIP — production hardening & scaling guide

Recommendations to take the voice-call stack (`voip/` + `voip/bridge`) from a
validated prototype to a production, multi-tenant service inside AtendZappy.

> The demo `voip-bridge-server` is a **single-session, unauthenticated** example.
> Production must integrate the `voip.Manager` into the authenticated backend and
> add the pieces below. Do **not** expose the demo server publicly.

---

## 0. The one architectural fact that drives everything

Unlike the rest of a REST API, VoIP here is **stateful and CPU-bound**:

- **Session affinity.** Each WhatsApp account is one `*whatsmeow.Client` = one
  Signal device session. A call for account A can only be handled by the process
  that holds A's session. You cannot freely load-balance calls across nodes.
- **CPU per active call.** The MLow codec (pure-Go CELP encode/decode) and the
  pion relay run per active call. This — not I/O — is the scaling ceiling.
  **Measure CPU-per-concurrent-call first**; it sets your per-node capacity.
- **Media can't be paused/migrated.** A live call is pinned to its worker for its
  whole duration.

Consequence: scale by **sharding accounts across workers** with a session-affinity
router, and do **capacity planning by CPU**, not by request rate.

---

## 1. Priority tiers

| Tier | Gate | Items |
|---|---|---|
| **P0** — before any production pilot | correctness, security, ban-safety | HTTP auth, TURN/STUN, per-tenant opt-in + ban monitoring, shared session store, basic metrics + CDR, integrate into the authenticated backend |
| **P1** — before scaling past one node | reliability at scale | multi-session manager, session-affinity routing, capacity planning, relay reconnect, timeouts, backpressure, graceful shutdown, CI for `voip/` |
| **P2** — quality & growth | UX & efficiency | adaptive jitter/PLC, VAD/silence suppression, autoscaling, load tests, (optional) video |

---

## 2. Ban risk & compliance — read this first

This is the highest-impact risk, above any technical item.

- **Reverse-engineered protocol.** Placing/answering calls uses undocumented
  crypto and constants. WhatsApp can change them server-side and **break all
  calls with no warning**, and can **ban accounts** for unofficial VoIP.
- **Recommendations:**
  - Keep `Config.Enabled` **default false**; enable **per tenant, opt-in**.
  - **Gradual rollout** with a per-account call **rate limit** and a **kill switch**.
  - **Monitor the ban rate** and known failure signatures (`error 479` spikes,
    mass call failures) → alert + auto-disable a tenant on threshold.
  - Avoid cold-calling at scale (spam pattern → bans). Prefer user-initiated /
    consented calls.
  - **Pin `whatsmeow`**; have a fast rollback path when a WhatsApp update lands.
  - Get a **business/legal sign-off** — this is against WhatsApp ToS.

Treat VoIP as a **controlled feature**, not a default-on capability.

---

## 3. P0 — before a production pilot

### 3.1 Authentication & authorization
- Put the signaling API behind the **existing AtendZappy auth** (JWT/session).
  Drop the demo server; drive `voip.Manager` from the authenticated backend.
- **Per-call authz:** an operator may only bridge/accept/hang up a call they own.
  Bind `call_id` → operator identity server-side; check on every endpoint,
  including `/webrtc` and the SSE stream.
- SSE/WebRTC endpoints are long-lived — authenticate at open and on reconnect.
- Rate-limit call creation per tenant/operator.

### 3.2 ICE — STUN/TURN for the browser leg
- Today `iceServers: []` works only on localhost/LAN. Real operators sit behind
  NATs/corporate firewalls.
- **STUN** for public-IP discovery; **TURN** (relays media) for symmetric NAT and
  restrictive networks — essential for call centers.
- Set `iceServers` on **both** legs: the browser `RTCPeerConnection` **and** the
  server-side pion `webrtc.Configuration` (currently empty in `bridge.New`).
- Options: self-hosted **coturn**, or managed (Cloudflare Calls, Twilio, Xirsys,
  Metered). TURN is **bandwidth-heavy** — size and monitor it.
- Use **short-lived TURN credentials** minted per session, not static secrets.

### 3.3 Shared, durable session store
- The demo uses **SQLite** (single file, single node). For multi-node / HA, move
  the whatsmeow store to **Postgres** (whatsmeow's `sqlstore` supports it) so
  sessions are shared and a worker can be recovered/rescheduled.
- Back up the session store; losing it means re-pairing every account.

### 3.4 Observability & CDR (minimum viable)
- **Metrics** (Prometheus): active calls, call-setup latency, ICE success rate,
  relay RTT, audio frame drops, per-tenant call counts, `error 479` rate.
- **Structured logs** correlated by `call_id` + `session_id` + tenant.
- **Call Detail Records** (start/answer/end, direction, peer, duration, reason)
  for billing, audit, and quality analysis.

---

## 4. P1 — reliability at scale

### 4.1 Multi-session manager + routing
- Host **many `voip.Manager`s** (one per account) in a worker; the current
  `MaxConcurrentCalls` guard is per-manager — keep it.
- Add a **session-affinity router**: incoming API calls and inbound-call events
  for account A must reach the worker holding A. Options: consistent-hashing on
  account id, a registry (etcd/Redis) mapping account→worker, or a control plane.
- Port the reference `SessionManager` (WaCalls `cmd/server/sessionmanager.go`) as
  the multi-account host; keep the per-call bridge registry (already have).
- **Implemented:** `voip/host` (multi-session worker) + `voip/router`
  (consistent-hash affinity ring) — see `docs/voip_multisession.md`.

### 4.2 Capacity planning
- Benchmark **CPU per concurrent active call** (MLow encode+decode + pion). Derive
  **max calls/worker** with headroom (target ~60–70% CPU at peak).
- Cap calls/worker; reject/queue past the cap (fail fast, don't degrade all calls).
- Separate **idle account sessions** (cheap: keepalive only) from **active calls**
  (expensive) in the capacity model.

### 4.3 Resilience
- **Relay reconnect:** handle WhatsApp relay drops mid-call — re-establish or end
  cleanly with a clear reason (WaCalls' relay manager has hooks; extend it).
- **Timeouts:** ring/no-answer timeout, max call duration, idle/silence timeout.
- **Backpressure:** never block the call goroutine on a stalled DataChannel or
  relay — use bounded buffers with drop-oldest (the playback worklet already ring-
  buffers; mirror that server-side).
- **Lifecycle hygiene:** guarantee cleanup of per-call goroutines (relay,
  keepalive) and bridges on **every** exit path (end, reject, ICE-fail, shutdown).
- **Graceful shutdown:** drain active calls, notify operators, persist sessions.

### 4.4 CI for the companion module
- The `Go` workflow builds/tests the **main** module only (Go skips nested
  modules). Add a job that runs `go build/vet/test ./...` inside `voip/` (incl. the
  MLow golden-vector tests). Add **bridge integration tests** (SDP exchange, PCM
  round-trip) and a **concurrent-call load test** that reports CPU.

---

## 5. P2 — quality & growth

- **Media quality:** adaptive jitter buffer + packet-loss concealment; **VAD /
  silence suppression** (MLow already has VAD) to cut bandwidth; handle clock
  drift between the browser `AudioContext` and WhatsApp's 16 kHz. Browser
  `getUserMedia` gives echo-cancellation/noise-suppression by default — keep it on.
- **Autoscaling:** scale workers on CPU + active-call count; respect session
  affinity (drain, don't hard-kill, workers with live calls).
- **Video (optional):** the reference `feat/video-calls` branch adds H.264 — even
  heavier CPU and ban exposure. Defer unless there's a concrete need; re-plan
  capacity if adopted.

---

## 6. Suggested target architecture

```
Browsers (operators)  ──WebRTC(audio)──►  TURN/STUN  ◄──►  VoIP workers
        │  auth + signaling (HTTPS/WSS/SSE)                 │  (hold whatsmeow
        ▼                                                   │   sessions, run
  AtendZappy API (auth, tenants, CDR)  ──affinity router──► │   MLow + pion)
        │                                                   ▼
        └──────────────► Postgres (whatsmeow sessions, CDR) ◄── WhatsApp relays
```

- **AtendZappy API**: auth, tenant management, call authz, CDR, affinity routing.
- **VoIP workers**: stateful, CPU-bound; hold sessions + run media; shardable.
- **TURN/STUN**: browser-leg NAT traversal (WhatsApp relays handle the WA leg).
- **Postgres**: shared session store + CDR.

---

## 7. Recommended sequence

1. Integrate `voip.Manager` into the authenticated backend; add per-call authz. (P0)
2. Stand up STUN/TURN; wire `iceServers` on both legs. (P0)
3. Move sessions to Postgres; add metrics + CDR. (P0)
4. Per-tenant opt-in + ban-rate monitoring + kill switch. (P0)
5. Pilot with a few consenting tenants on disposable/warm accounts; watch bans. (P0)
6. Multi-session host + affinity router + capacity limits. (P1)
7. Reconnect/timeouts/backpressure/graceful shutdown + CI job. (P1)
8. Quality (jitter/PLC/VAD) and autoscaling; video only if needed. (P2)

**Bottom line:** the call mechanics are proven. The scaling work is mostly the
*operational envelope* around a stateful, CPU-bound, ban-sensitive core —
auth, NAT traversal, session sharding by CPU, resilience, and tight ban
monitoring — not more call protocol.
