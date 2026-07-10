# VoIP media — field test report (disposable accounts)

Manual end-to-end test of the ported media stack (MLow + SRTP + pion/SCTP relay
+ `CallManager`) via `voip/examples/voip-call`, on **disposable accounts only**.
Date: 2026-07-10.

- **Our-lib account (A):** `557774004885` (reused the interactive-test session).
- **Peer disposable phone (B):** `557788272902`.

Both directions succeeded with **real bidirectional audio** over the WhatsApp
relay, with **no** `error 479` / `Failed to encrypt` / `Failed to send` /
`SQLITE_BUSY` / panic in the logs.

## A) Inbound — B calls our lib (auto-answer)

`VOIP_ENABLED=1 VOIP_ACCEPT=1 go run ./examples/voip-call`

Call `00343D85…2303B`:

```
INFO offer relays parsed via structured (te2) format relays=3
📲 incoming call … from 258080709300320@lid
📞 incoming_ringing → connecting
INFO call accepted
INFO relay ice state … state=checking → connected
INFO relay datachannel open
📞 → active
🔊 peer audio: 50 … 100 frames (960 samples)
🔚 ended: user_ended (peer frames=139)
```

- Offer detected, call key decrypted, accept sent, 3 relays negotiated, ICE
  connected, datachannel open, state `active`.
- **139 audio frames** from B decrypted (SRTP) and decoded (MLow) on our side
  while we streamed a 440 Hz test tone back.

## B) Outbound — our lib calls B

`VOIP_ENABLED=1 VOIP_CALL=557788272902 VOIP_SECS=30 go run ./examples/voip-call`

Call `D271C977…50C6`:

```
📞 → ringing
INFO call offer sent peer=92694269452481@lid
INFO offer ack received relays=3 participants=1
INFO relay ice state … state=checking → connected
INFO relay datachannel open   (x2)
INFO remote accepted call … relay_connected=true relay_endpoints=3
📞 → active
🔊 peer audio: 50 … 100 frames (960 samples)
🔚 ended: user_ended (peer frames=103)
```

- **9th-digit note:** `557788272902` (no leading 9) resolved correctly to LID
  `92694269452481@lid` — no dialing-prefix issue.
- Offer accepted by the server (ack, 3 relays), **B answered** (`remote accepted
  call`), state `active`, **103 audio frames** from B decoded via MLow.

## Verdict

The WaCalls call stack is **operational in the fork**: detect → decrypt call key
→ negotiate relay (pion/ICE) → establish SRTP → exchange MLow-encoded audio, in
both directions, validated live. Kept behind `Config.Enabled` (default false);
disposable accounts only. See `docs/voip_media.md` for the API and packaging.
