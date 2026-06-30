# Bot-node patch — field test report

**Date:** 2026-06-30
**Branch:** `feature/interactive-bot-node` (fork `williamprado/whatsmeow`)
**Patch:** inject `<bot biz_bot="1"/>` for private 1:1 interactive messages, relocate
`biz` after `tctoken`, `native_flow v="9"`, name map (commit `65e3165`).
**Sender:** disposable test account (linked device, **non-Business**).
**Recipient:** `557798020125` (canonical JID via `IsOnWhatsApp`).

## Emitted stanza (audit log)

The patch logs the cleartext `biz`/`bot` nodes it appends (at INFO). Captured from
this run:

```
Buttons:   biz=<biz><buttons/></biz>                                                              bot=<bot biz_bot="1"/>
List:      biz=<biz><list type="single_select" v="2"/></biz>                                       bot=<bot biz_bot="1"/>
Template:  biz=<biz><interactive type="native_flow" v="1"><native_flow name="mixed" v="9"/></interactive></biz>  bot=<bot biz_bot="1"/>
NativeFlow:biz=<biz><interactive type="native_flow" v="1"><native_flow name="mixed" v="9"/></interactive></biz>  bot=<bot biz_bot="1"/>
Carousel:  biz=<biz><interactive type="native_flow" v="1"><native_flow name="mixed" v="9"/></interactive></biz>  bot=<none>
```

Confirms the patch is correct against the spec:
- `<bot biz_bot="1"/>` appended after `<biz>` for quick_reply / CTA / list / template.
- **No** bot node for carousel (`bot=<none>`).
- `native_flow v="9"`, `name="mixed"` for quick_reply+cta, `interactive v="1"`.
- Ordering: `… → tctoken → biz → bot` (biz relocated to the end).

## Send-level results

All six messages sent successfully — **no 479** (Buttons `3EB0B6B14830A5A287A1FD`,
List `3EB02B03258BC0A487EB91`, Template `3EB0E0499269F51A786831`, NativeFlow
`3EB0E20AA899116D24A663`, Carousel `3EB0DB3E99E2AE03C58F43`).

## Rendering results on the recipient device

| Message | Before bot node (prev. report) | After bot node |
| --- | --- | --- |
| ControlText (plain) | ✅ text | ✅ text |
| **InteractiveMessage (native flow)** | ✅ clickable buttons | ✅ clickable buttons (Yes + Site), now with an **"IA ✦" badge** — the bot node makes WhatsApp treat it as a business/AI message |
| **ButtonsMessage** | ⚠️ plain text | ❌ **did not arrive / not shown** |
| **ListMessage** | ⚠️ plain text | ❌ **did not arrive / not shown** |
| TemplateMessage | ⚠️ weak | ❌ not shown / "version not compatible" |
| CarouselMessage | ❌ "version not compatible" | ❌ "version not compatible" |

User confirmation: *"a lista e os botões simples simplesmente não chegaram"* (the
list and the simple buttons simply didn't arrive).

## Key finding

**The bot node did NOT make our list render — it makes legacy `ButtonsMessage`
and `ListMessage` get silently dropped by the recipient.** Only the **native_flow
`InteractiveMessage`** renders (and the bot node visibly upgrades it to a
business/"IA" interactive card).

Why: the reference (Baileys) never sends legacy `ButtonsMessage`/`ListMessage`.
Its "list" is a **native_flow `single_select`** interactive message. The
`<bot biz_bot="1"/>` node tells the client "this is a native-flow business
interactive message"; when the actual `<biz>` content is the legacy
`<buttons>` / `<list>` form instead of `<interactive><native_flow>`, the client
rejects/drops it. So pairing the bot node with our legacy list/buttons is a
mismatch — and is **worse** than before (dropped vs. text fallback).

## Recommendation (for review before any promotion)

1. **Scope the bot node to native_flow messages only** (InteractiveMessage), OR
2. **Build lists/buttons as native_flow** to match the reference: send a list as
   an `InteractiveMessage` whose native-flow button is `single_select` (with the
   sections/rows in `ButtonParamsJSON`), and quick replies as native-flow
   `quick_reply` buttons — instead of the legacy `BuildListMessage` /
   `BuildButtonsMessage`. Then the bot node applies cleanly and the list should
   render like the reference.
3. Until then, **use `BuildInteractiveMessage` (native flow) only**; it is the one
   format confirmed to render end-to-end (buttons clickable + response received).

⚠️ Still **experimental / account-ban risk** — disposable accounts only; never the
production (atendzappy) account. Do not promote without Tech Lead sign-off and an
evaluation of the official WhatsApp Business Cloud API.

## Reproduce

```sh
export TEST_RECIPIENT=557798020125
go run ./examples/interactive-test   # logs "Interactive stanza nodes: biz=... bot=..." at INFO
# LISTEN_ONLY=1 to capture taps; CHECK_ONLY=1 to only resolve the number
```
