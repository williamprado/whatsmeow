# VoIP Phase 1 (part 1) — companion module + call-key crypto + signaling

> **Status: incremental Phase 1.** This part delivers the separate
> `whatsmeow-voip` companion module, the call-key crypto, and the offer/accept
> signaling + relay-ack parsing — enough for a real **call handshake** (verifiable
> without audio). **Media (MLow/SRTP/STUN/pion transport) is the NEXT PR** (where
> the bidirectional-audio test with two disposable accounts happens).
>
> ⚠️ **VERY HIGH account-ban risk.** This uses reverse-engineered crypto and
> protocol constants. Everything stays behind `Config.Enabled` (default false) and
> is tested only on disposable accounts — never production, never the production pin.

Reference: `williamprado/WaCalls` (module `wacalls`), pinned commit
**`fc7b8c32c96b10710cc5325c312546f2778f7d97`** (its own whatsmeow pin:
`v0.0.0-20260622185415-5f04eac6dbbb`).

## 1. Packaging — separate `whatsmeow-voip` module

The VoIP code now lives in its **own Go module** under `voip/`, so the heavy media
dependency tree (pion/webrtc, MLow, SRTP/STUN — added in the media PR) never
touches the main `go.mau.fi/whatsmeow` `go.mod`.

- **Module path:** `github.com/williamprado/whatsmeow/voip` (nested module in this
  repo — no new repository was created).
- **whatsmeow it consumes:** `replace go.mau.fi/whatsmeow => ../`, i.e. the local
  parent fork checkout. The require line is the placeholder
  `v0.0.0-00010101000000-000000000000` (resolved by the replace). **The production
  pin is not touched** — this module always builds against this repo's whatsmeow.
- **How the main module references it:** it does **not**. The main module has no
  dependency on the companion (that is the whole point — isolation). Consumers who
  want calls import `github.com/williamprado/whatsmeow/voip` separately.
- WaCalls itself is still **not** a dependency (non-canonical module path `wacalls`
  + everything under `internal/`); the relevant logic was **lifted/ported** from
  the pinned commit, not imported.

Layout:

```
voip/                     # module github.com/williamprado/whatsmeow/voip
  go.mod  (replace go.mau.fi/whatsmeow => ../)
  voip.go            # Manager: receive, DecryptCallKey, StartCall, Reject/Terminate
  socket.go          # VoipSocket interface + adapter (plumbing + crypto)
  client_adapter.go  # wiring to Client.DangerousInternals() + public API
  callkey.go         # call key gen + waE2E.Call encode/decode
  signaling.go       # BuildOfferStanza / BuildAcceptStanza / ParseRelayFromAck
  *_test.go
  examples/voip-receive/   # manual test (behind VOIP_ENABLED)
```

> **CI note:** the existing `Go` workflow builds/tests the **main** module only;
> it does not descend into the nested `voip/` module (Go skips nested modules).
> Both modules are green locally (`go build/vet/test ./...`, `go mod tidy` no-op,
> native pre-commit). Adding a CI job for the companion module needs a workflow
> edit — flagged for the Tech Lead (I did not modify Actions config per the
> constraints). The media PR will add the MLow golden-vector tests and should come
> with that CI job.

## 2. Call-key crypto (replaces the Phase-0 stubs)

`socket.go` now implements (over `Client.DangerousInternals()`):

- **`CreateParticipantNodes`** — wraps the 32-byte call key in
  `waE2E.Message{Call:{CallKey}}`, then `EncryptMessageForDevices` to produce the
  per-device `<enc>` participant nodes for `<destination>`.
- **`DecryptCallKey`** — `DecryptDM` the peer's `<enc>` (`pkmsg` ⇒ prekey), then
  unmarshal `waE2E.Message.Call.CallKey` (validates 32 bytes).

Unit tests cover the encode/decode roundtrip and the encrypt→decrypt roundtrip
through a fake client API.

## 3. Signaling (offer / accept / relay-ack)

`signaling.go`:

- **`BuildOfferStanza`** — `OwnLID`/`OwnPN` creator, `GetUSyncDevices`,
  `AssertSessions`, `CreateParticipantNodes` → `<destination>`, plus `<audio>`
  (opus 8k+16k), `<net medium=3>`, `<capability>`, `<encopt keygen=2>`, the
  privacy/TC token (`GetTCToken`) and `<device-identity>`. Wrapped as
  `<call to=peer id=…><offer call-id call-creator>…</offer></call>`.
- **`BuildAcceptStanza`** — re-encrypts the call key for the creator and assembles
  `<accept>` (audio 16k, net, `<enc>`, encopt, device-identity).
- **`ParseRelayFromAck`** — parses the synchronous offer ack into
  `ParsedRelayAck{Relays[], ParticipantJids, UUID, SelfPid, PeerPid, HbhKey}`,
  including `<te2>` IPv4:port endpoints, tokens/auth-tokens and the 30-byte
  hop-by-hop key.

`Manager.StartCall(ctx, peer)` ties it together: build offer → `Query` (wait for
ack) → `ParseRelayFromAck` → log relays. **No media** is set up.

## 4. Reverse-engineered / hard-coded protocol constants (risk review)

These have no official spec and WhatsApp can change them server-side, breaking
calls with no version bump. Ported verbatim from the reference:

- `<capability ver="1">` offer blob: `01 05 f7 09 e4 bb 07` (preaccept variant
  `01 05 ff 09 e4 bb 07`).
- `<audio enc="opus">` rates `8000` (offer) + `16000`; `<net medium="3">`
  (transport uses `medium="2" protocol="0"`); `<encopt keygen="2">`.
- `CreateParticipantNodes` enc attr `{"count":"0"}`.
- Relay ack: `hbh_key` raw length **30** (else base64→30); `<te2>` address = 6 bytes
  (4-byte IPv4 + 2-byte big-endian port); default token id `"0"`.
- Call key = 32 random bytes; call-id / stanza-id = 16 random bytes uppercase hex.
- **For the media PR (not used yet, listed for completeness):** `WARelayPort=3480`,
  `PayloadTypeWhatsAppOpus=120`, SRTP auth tag = 4 bytes, static
  `WADTLSFingerprint = sha-256 F9:CA:0C:98:…:A0:68`, SRTP per-JID key = HKDF-SHA256
  (ikm=callKey, salt=nil, info=deviceJid, L=46 → 16-byte key + 14-byte salt), SSRC
  = HKDF-SHA256 (ikm=callID, salt=LE32(counter), info=selfJid, L=4).

## 5. Manual test (disposable accounts)

```sh
cd voip
# A) verify call-key DECRYPTION on a real incoming call + clean reject:
VOIP_ENABLED=1 go run ./examples/voip-receive
#   then call the disposable account from a 2nd phone → logs "🔑 decrypted call key: 32 bytes" + "✅ rejected".
# B) verify the OUTGOING offer handshake (no audio) to a 2nd disposable number:
VOIP_ENABLED=1 VOIP_CALL=<2nd-disposable-number> go run ./examples/voip-receive
#   logs "✅ offer handshake ok: callID=…, N relay endpoint(s)" then terminates.
```

`SESSION_DB` overrides the reused session path; no number is hardcoded
(`TEST_RECIPIENT`/`VOIP_CALL` via env).

## What's next (media PR)

Port MLow (pure-Go), the hand-rolled SRTP/STUN, and the pion/webrtc DataChannel
relay transport into this module (adding pion to the companion `go.mod` only);
SSRC/keys via HKDF; one-way → bidirectional audio; relay reconnect; MLow
golden-vector tests + a CI job for the companion module. Bidirectional-audio test
between two disposable accounts happens there.
