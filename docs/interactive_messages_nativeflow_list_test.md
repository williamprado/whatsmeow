# Native-flow list / quick-reply — field test report

**Date:** 2026-06-30
**Branch:** `feature/native-flow-list` (base `feature/interactive-bot-node`)
**Sender:** disposable test account (linked device, **non-Business**).
**Recipient:** `557798020125` (canonical JID via `IsOnWhatsApp`).

## Goal

Replace the legacy `ButtonsMessage` / `ListMessage` (dropped by recipients, PR #2)
with the renderable native-flow formats the reference uses: a `single_select`
list and `quick_reply` buttons, both as `InteractiveMessage`.

## `single_select` buttonParamsJSON (audit)

```json
{"title":"Open menu","sections":[
  {"title":"Section A","rows":[
    {"id":"a1","title":"A1","description":"first"},
    {"id":"a2","title":"A2","description":""}]},
  {"title":"Section B","rows":[
    {"id":"b1","title":"B1","description":""}]}]}
```

Plus `messageParamsJSON:"{}"`, `messageVersion:2`, button `name:"single_select"`,
all inside an `InteractiveMessage` wrapped in `ViewOnceMessage`.

## Emitted stanza (biz/bot audit log)

```
Legacy list:   biz=<biz><list type="single_select" v="2"/></biz>                                                 bot=<bot biz_bot="1"/>
NativeFlowList:biz=<biz><interactive type="native_flow" v="1"><native_flow name="mixed" v="9"/></interactive></biz>  bot=<bot biz_bot="1"/>
QuickReply:    biz=<biz><interactive type="native_flow" v="1"><native_flow name="mixed" v="9"/></interactive></biz>  bot=<bot biz_bot="1"/>
Baseline:      biz=<biz><interactive type="native_flow" v="1"><native_flow name="mixed" v="9"/></interactive></biz>  bot=<bot biz_bot="1"/>
```

## Send-level results

All sent — **no 479**, zero `SQLITE_BUSY`: Control `3EB074ED827C5A7D561AFD`,
LegacyList `3EB00441CE424257C6F5E2`, NativeFlowList `3EB055A7751E483453564B`,
QuickReply `3EB0EAA154B1E1D9923B52`, Baseline `3EB06AB2911BFDAD9AC34F`.

## Rendering results on the recipient device ✅

| Message | Rendered? |
| --- | --- |
| **NativeFlowList (single_select)** | ✅ **"Open menu" list button** (☰), opens Section A / Section B with selectable rows |
| **NativeFlowQuickReply** | ✅ **Yes / No / Maybe** clickable buttons |
| InteractiveMessage baseline (Yes/Site) | ✅ clickable |
| **LegacyListMessage** | ❌ **dropped / not shown** (as in PR #2) |
| ControlText | ✅ text |

All interactive cards show the **"IA ✦"** badge (effect of the `<bot biz_bot="1"/>`
node). Direct comparison in one run: the native-flow list **renders** while the
legacy `ListMessage` sent right before it **vanished**.

## Response capture (single_select)

Tapping **Open menu** and selecting row **A2** produced, on the sender's listening
client:

```
↩️  interactiveResponse(nativeFlow) from <recipient-lid>: name="menu_options" paramsJSON={"id":"a2","description":""}
```

So a list selection round-trips as an `InteractiveResponseMessage` /
`NativeFlowResponseMessage` with `name="menu_options"` and the selected row `id`.

## Conclusion

**Sending lists and quick replies as native-flow `InteractiveMessage` (single_select /
quick_reply), wrapped in ViewOnce, renders end-to-end** — the list opens with its
sections and the selected row id comes back. This is the format to use; the legacy
`BuildButtonsMessage` / `BuildListMessage` are deprecated (dropped by recipients).

⚠️ Still **experimental / account-ban risk** — disposable accounts only; never the
production (atendzappy) account. Do not promote without Tech Lead sign-off and an
evaluation of the official WhatsApp Business Cloud API.

## Reproduce

```sh
export TEST_RECIPIENT=557798020125
go run ./examples/interactive-test   # logs buttonParamsJSON + biz/bot stanza at INFO
# LISTEN_ONLY=1 to capture the single_select response when a row is selected
```
