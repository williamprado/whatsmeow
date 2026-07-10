# VoIP media — full audio call stack (MLow + SRTP + relay transport)

> **Status: media phase complete.** The companion `voip/` module now carries the
> entire voice-call stack ported from the reference
> [`williamprado/WaCalls`](https://github.com/williamprado/WaCalls)
> (upstream `JotaDev66/WaCalls`, branch `feat/native-mlow-pcm-transport`): the
> MLow audio codec, SRTP, the pion/SCTP relay transport, and the call state
> machine. This supersedes the Phase-1 "part 1" hand-port (call-key crypto +
> signaling only) — those files were replaced by the vendored packages.
>
> ⚠️ **VERY HIGH account-ban risk.** Reverse-engineered crypto and protocol
> constants, plus a reverse-engineered proprietary audio codec. Everything stays
> behind `Config.Enabled` (default false) and is exercised only on disposable
> accounts — never production, never the production pin.

## 1. What was integrated

The whole `internal/voip/` tree from the reference was **vendored** into this
fork's companion module and the WaCalls-internal socket adapter was lifted too.
Import paths were rewritten `wacalls/internal/voip/… → github.com/williamprado/whatsmeow/voip/…`.

| Package (`voip/…`) | Origin | Role |
|---|---|---|
| `media/mlow/` | `internal/voip/media/mlow` | Pure-Go MLow (CELP) encoder/decoder + golden-vector tests |
| `media/` | `internal/voip/media` | SRTP, RTP, SSRC (HKDF), resample, PCM, codec wrapper |
| `transport/` | `internal/voip/transport` | Relay transport (pion/SCTP) + STUN + subscriptions |
| `signaling/` | `internal/voip/signaling` | offer/accept/reject/terminate build + parse, relay-ack, call-key codec |
| `wanode/` | `internal/voip/wanode` | binary-node/JID helpers |
| `core/` | `internal/voip/core` | `VoipSocket` interface, shared types, protocol constants |
| `call/` | `internal/voip/call` | `CallManager` state machine + media/relay/srtp wiring |
| `wa/` | `internal/wa/socket.go` | `core.VoipSocket` implementation over `*whatsmeow.Client` |

The **top-level `voip` package** (`voip.go`) is fork-specific glue: it wires a
`*whatsmeow.Client` to a per-call `call.CallManager` via `wa.Socket`, bridges the
fork's `events.Call*` into the state machine, and exposes a small public API.

## 2. Packaging & dependencies

- Still a **separate module** `github.com/williamprado/whatsmeow/voip` with
  `replace go.mau.fi/whatsmeow => ../`. The heavy media deps live **only** here.
- **New dependency:** `github.com/pion/webrtc/v4` (pulls pion sctp/srtp/stun/dtls/…)
  — used by `transport/sctprelay.go`. The **main module `go.mod` is untouched.**
- The fork already emits every needed event
  (`CallOffer/CallAccept/CallPreAccept/CallTransport/CallTerminate/CallReject`,
  each with `Data *waBinary.Node`), so **no whatsmeow core change was required.**

## 3. Public API (top-level `voip` package)

```go
mgr := voip.New(client, voip.Config{Enabled: true, MaxConcurrentCalls: 1}, nil)
mgr.OnIncomingCall(func(c *call.CallInfo) { /* ring */ })
mgr.OnCallStateChange(func(c *call.CallInfo) { /* state */ })
mgr.OnPeerAudio(func(callID string, pcm16 []float32) { /* 16 kHz mono */ })
mgr.Start() // subscribe to client events

callID, _ := mgr.StartCall(ctx, peer, false) // outbound audio call
mgr.FeedCapturedPCM(callID, micFrame)         // 16 kHz mono float32, 20 ms frames
// ... or, for inbound:
mgr.AcceptCall(ctx, callID) / mgr.RejectCall(ctx, callID, reason)
mgr.EndCall(ctx, callID, reason)
```

- **Audio in:** `FeedCapturedPCM(callID, []float32)` → MLow encode → SRTP → relay.
- **Audio out:** `OnPeerAudio(callID, []float32)` decoded from the relay.
- When `Config.Enabled` is false, `StartCall`/`AcceptCall` return `ErrDisabled`
  and inbound offers are auto-rejected (still surfaced via `OnIncomingCall`).

## 4. Validation

- `voip/` module: `go build ./...`, `go vet ./...`, `go test ./...` all green,
  **including** the MLow codec golden-vector tests and the SRTP/SSRC/signaling/
  transport suites. `gofmt` clean.
- Main module: `go build/vet/test` unaffected (Go skips the nested module).
- CI note (unchanged): the `Go` workflow builds/tests the **main** module only;
  a CI job for the companion module still needs a workflow edit — flagged for the
  Tech Lead (Actions config not modified per the constraints).

## 5. Manual test (disposable accounts only)

```sh
cd voip
# Receive + auto-answer, feeding a 440 Hz test tone back:
VOIP_ENABLED=1 VOIP_ACCEPT=1 go run ./examples/voip-call
#   → 📲 incoming, 📞 state=active, 🔊 peer audio frames, 🔚 ended

# Place a call to a 2nd disposable number for 15 s of tone:
VOIP_ENABLED=1 VOIP_CALL=<2nd-disposable-number> VOIP_SECS=15 go run ./examples/voip-call
#   → 📤 placed, 📞 state=active, 🔊 peer audio frames
```

`SESSION_DB` overrides the session path; no number is hardcoded. The example
feeds a synthetic tone via `FeedCapturedPCM`; a real integration would feed
microphone PCM and play `OnPeerAudio` frames.

## 6. Risk register (unchanged constants, now with a codec)

Everything from `voip_phase1.md` §4 still applies (capability blobs, audio rates,
`net medium`, `encopt keygen`, `<te2>` relay endpoints, `WARelayPort=3480`,
`PayloadTypeWhatsAppOpus=120`, `WADTLSFingerprint`, HKDF SRTP/SSRC derivations).
The media phase adds the **MLow codec** — a reverse-engineered proprietary CELP
codec validated against captured golden vectors. WhatsApp can change any of these
server-side and break calls with no version bump. Keep it flagged off by default.
