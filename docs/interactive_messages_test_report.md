# Interactive buttons — field test report (before vs after the core patch)

**Date:** 2026-06-29
**Branch:** `feature/interactive-helpers` (fork `williamprado/whatsmeow`)
**Sender:** disposable test account (linked device via QR, **non-Business**)
**Recipient:** `557798020125` (canonical JID resolved via `IsOnWhatsApp`)
**Driver:** pure-Go `modernc.org/sqlite`

This documents three runs of `examples/interactive-test`:

1. **Before patch** — upstream behavior (no `<biz>` node for Template/Interactive).
2. **Patch v1** — single-level node `<biz><interactive type="native_flow" v="1"/></biz>`.
3. **Patch v2 (nested)** — full `<biz><interactive type="native_flow" v="1"><native_flow name="..." v="2"/></interactive></biz>`.

## Send-level results (API)

| Message | Before patch | Patch v1 (single-level) | Patch v2 (nested) |
| --- | :---: | :---: | :---: |
| ControlText (plain) | ✅ sent | ✅ sent | ✅ sent |
| ButtonsMessage | ✅ sent | ✅ sent | ✅ sent |
| ListMessage | ✅ sent | ✅ sent | ✅ sent |
| TemplateMessage | ✅ sent | ❌ **479** | ✅ sent |
| InteractiveMessage (native flow) | ✅ sent | ❌ **479** | ✅ sent |
| CarouselMessage | ✅ sent | ❌ **479** | ✅ sent |

**Key send finding:** the single-level interactive node is **rejected by the server with error 479**. Adding the nested `<native_flow>` grandchild fixes it — all types send again. (The earlier "479" seen on the very first login run was a different, unrelated cause: local `SQLITE_BUSY` during the post-login sync storm. The clean runs had zero `SQLITE_BUSY`.)

## Rendering results on the recipient device

| Message | Before patch | After patch v2 (nested) |
| --- | --- | --- |
| ControlText (plain) | ✅ readable text | ✅ readable text |
| **InteractiveMessage (native flow)** | ❌ "version not compatible" / "waiting" | ✅ **renders clickable buttons** (Yes + Site); tap returns a response |
| ButtonsMessage | ❌ "waiting for this message" | ⚠️ rendered as plain text (no clickable buttons) |
| ListMessage | ❌ plain-text fallback | ⚠️ plain-text fallback |
| TemplateMessage | ❌ "version not compatible" | ⚠️ plain text / weak |
| CarouselMessage | ❌ "version not compatible" | ❌ still "version not compatible" on the test client |

### Evidence — native flow buttons rendered and the tap was captured

The recipient saw a native-flow card with a header, body, footer and two buttons
(**Yes** quick-reply + **Site** URL). Tapping **Yes** produced, on the sender's
listening client:

```
↩️  templateButtonReply from 258080709300320@lid: selectedID="nf-yes" displayText="Yes" index=0
```

So `BuildInteractiveMessage` (native flow) is confirmed end-to-end: it sends,
renders clickable buttons, and the response (button ID + display text) is
received.

## Conclusions

1. **The core patch matters and works — for native flow.** Before the patch,
   `InteractiveMessage` showed "version not compatible". After the nested patch
   it renders real clickable buttons and round-trips the response. This is the
   recommended path for buttons that must render.
2. **The node must be the nested form.** The single-level `<interactive>` node
   (no `<native_flow>` child) is rejected by the server with 479. The grandchild
   is mandatory.
3. **JID normalization is mandatory in Brazil.** With the dialed 9th-digit number
   (`5577988272902`) the server accepted sends but delivered nothing, because the
   canonical JID is `557788272902` (no 9). `Client.ResolveRecipientJID`
   (`IsOnWhatsApp`) fixes this and must be used before sending.
4. **The other types are weaker.** `ButtonsMessage`/`ListMessage`/`TemplateMessage`
   tend to degrade to plain text, and `CarouselMessage` still showed
   "version not compatible" on the test client. Their reliability likely depends
   on the recipient's app version and/or sender status.

## Recommendation

- For interactive buttons via this fork, **use `BuildInteractiveMessage` (native
  flow)** with the nested patch — it is the one format confirmed to render and
  respond.
- Always resolve the recipient JID with `Client.ResolveRecipientJID` first.
- Treat carousel/template/quick-reply-buttons as **unreliable** for rendering.
- ⚠️ Still **experimental / account-ban risk**: WhatsApp may ban non-Business
  accounts that send buttons. Use disposable accounts only; do **not** enable in
  production (atendzappy) without Tech Lead sign-off and an evaluation of the
  official WhatsApp Business Cloud API.

## Reproduce

```sh
export TEST_RECIPIENT=557798020125     # resolved to canonical JID automatically
go run ./examples/interactive-test     # scan ASCII QR with a disposable account
# CHECK_ONLY=1  -> only resolve the number(s)
# LISTEN_ONLY=1 -> connect and log incoming button responses (tap to capture)
# QR_PNG=1      -> also write qr.png if the terminal can't render the ASCII QR
```
