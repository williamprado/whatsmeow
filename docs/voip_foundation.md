# VoIP foundation (Phase 0) — receive-only, behind a flag

> **Status: foundation only. NO live audio.** Phase 0 detects incoming calls and
> rejects/terminates them cleanly, behind a default-off flag. Sending offers,
> call-key crypto and media are **Phase 1** (not implemented here).
>
> ⚠️ **HIGH account-ban risk.** VoIP via an unofficial library uses
> reverse-engineered crypto and protocol constants — far more sensitive than
> interactive messages. Enable ONLY on a disposable test account; never the
> production (atendzappy) account. Do not deploy. Do not change the production
> whatsmeow pin.

Reference: **`williamprado/WaCalls`** (module `wacalls`), pinned at commit
**`fc7b8c32c96b10710cc5325c312546f2778f7d97`** (2026-06-30). WaCalls' own
whatsmeow pin is `v0.0.0-20260622185415-5f04eac6dbbb`.

## Why WaCalls is NOT a go.mod dependency (important)

WaCalls **cannot** be added as a normal `go.mod` require and imported by this fork:

1. **Module path is `wacalls`** — a non-canonical path Go cannot resolve to a VCS
   (`go get wacalls` fails; a `replace` to `github.com/williamprado/WaCalls` is
   rejected because the target's `go.mod` declares `module wacalls`).
2. **All call logic lives under `internal/`** (`internal/voip/**`, `internal/wa`).
   Go's `internal/` rule forbids importing those packages from another module
   (`go.mau.fi/whatsmeow`). The only non-internal Go code is `cmd/server`
   (package `main`, not importable).

Therefore Phase 0 does **not** import WaCalls. The pinned commit above is recorded
as the **reference revision to vendor/lift from in Phase 1** (copying
`internal/voip/**` + `internal/wa` into a package in this fork, or into a separate
companion module). Keeping the hash here — rather than in `go.mod` — also avoids
`go mod tidy` pruning an unused require (which previously broke CI).

## What Phase 0 ships (package `voip/`)

- **`voip/voip.go` — `Manager`.** Gated by `Config.Enabled` (default false).
  - `Start()` registers a handler **only when enabled** (no-op + log otherwise).
  - On `*events.CallOffer` it surfaces an `IncomingCall{CallID, From, CallCreator,
    Timestamp}` via `OnIncomingCall(cb)` and logs it. `*events.CallTerminate` /
    `*events.CallReject` are logged.
  - `Reject(ctx, call)` / `Terminate(ctx, call)` send a clean plaintext
    `<call to=…><reject|terminate call-id=… call-creator=…/></call>` (matching the
    reference). Disabled → `ErrDisabled`. **No audio, no crypto.**
- **`voip/socket.go` — `VoipSocket`** interface (mirrors WaCalls `core.VoipSocket`)
  and its adapter `socket`. Phase 0 implements the plaintext plumbing
  (`OwnPN/OwnLID/AccountDeviceIdentityNode/SendNode/Query/GetUSyncDevices/
  ResolveLIDForPN/GetTCToken`, `AssertSessions` no-op). The call-key crypto
  methods **`CreateParticipantNodes` and `DecryptCallKey` return `ErrVoIPPhase1`**.
- **`voip/client_adapter.go`** — the only file wiring `voip` to whatsmeow, over
  `Client.DangerousInternals()` (`SendNode`, `WaitResponse`/`CancelResponse`,
  `GetOwnID`/`GetOwnLID`, `MakeDeviceIdentityNode`) plus the public Client/Store
  API (`GetUserDevices`, `Store.LIDs.GetLIDForPN`, `GetUserInfo`,
  `Store.PrivacyTokens.GetPrivacyToken`). **No private whatsmeow internals are
  used** — everything is exported (`DangerousInternals` is whatsmeow's own facade).

### Behind the flag

With `Config.Enabled=false` (default): the Manager registers **no** event handler,
performs no I/O, and `Reject`/`Terminate` return `ErrDisabled`. The receiving
example only listens when `VOIP_ENABLED=1`.

### Receiving needs no whatsmeow core change

Incoming calls are delivered by whatsmeow's **public** events (`call.go` already
`dispatchEvent`s `CallOffer/Accept/Transport/Terminate/Reject`). Phase 0 only
consumes them; the send path for reject/terminate uses `DangerousInternals().SendNode`.

## Manual test (disposable account)

```sh
# The disposable account must already be paired (examples/interactive-test/session.db).
VOIP_ENABLED=1 go run ./examples/voip-receive
# Then place a WhatsApp call TO that account from ANOTHER phone.
# Expected: "📞 Incoming call: id=… from=…" then "✅ rejected call id=…".
```

`TEST_RECIPIENT` is not needed for receiving; no number is hardcoded.

## What's left for Phase 1 (after Tech-Lead approval)

1. **Packaging decision** (see `docs/calls_integration_plan.md`): vendor
   `internal/voip/**` into a `voip/...` subtree here, or a separate
   `whatsmeow-voip` module (isolates the pion/webrtc dependency tree from the
   main `go.mod`).
2. **Call-key crypto** — implement `CreateParticipantNodes`
   (`DangerousInternals().EncryptMessageForDevices`) and `DecryptCallKey`
   (`DecryptDM` + `waE2E.Call.CallKey`).
3. **Offer/accept signaling** — build `<offer>`/`<accept>` (USync devices,
   `<destination>`, `<encopt>`, privacy token) and parse the relay ack.
4. **Media** — port MLow (pure-Go), the hand-rolled SRTP/STUN, and the
   pion/webrtc DataChannel relay transport; SSRC/keys via HKDF; one-way then
   bidirectional audio.
5. **Robustness + tests** — relay reconnect, MLow golden-vector tests in CI.

⚠️ Reminder: each phase is its own branch/PR, **no merge without approval**, tested
only on disposable accounts. Strongly consider the official **WhatsApp Business
Calling API** before any real use.
