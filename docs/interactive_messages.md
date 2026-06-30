# Interactive (button) message helpers

This fork adds ergonomic constructors for WhatsApp's interactive message types in
[`interactive_helpers.go`](../interactive_helpers.go). They build the correct
`waE2E.*` protobuf structs from plain Go inputs so you can pass the result
straight to `Client.SendMessage`. No protocol code is ported — whatsmeow already
supports these message types natively; the helpers only wrap them in a friendlier
API.

> ⚠️ **EXPERIMENTAL — account-ban risk.** Sending interactive messages from
> non-official libraries is considered experimental. WhatsApp actively blocks
> non-business accounts from sending buttons and may **ban** accounts that do.
> Test only with disposable accounts in a development environment. **Do not
> enable in production (atendzappy) without explicit Tech Lead sign-off** and an
> evaluation of migrating to the official WhatsApp Business API.

## Helper overview

| Helper | Builds | `<biz>` routing node attached? |
| --- | --- | --- |
| `BuildButtonsMessage` | `waE2E.ButtonsMessage` (up to 3 quick-reply buttons) | ✅ `buttons` (upstream) |
| `BuildListMessage` | `waE2E.ListMessage` (single-select sections/rows) | ✅ `list` (upstream) |
| `BuildTemplateMessage` | `waE2E.TemplateMessage` (hydrated reply/url/call buttons) | ✅ `interactive`/`native_flow` (fork patch) |
| `BuildInteractiveMessage` | `waE2E.InteractiveMessage` (single native-flow group) | ✅ `interactive`/`native_flow` (fork patch) |
| `BuildCarouselMessage` | `waE2E.InteractiveMessage` with `CarouselMessage` | ✅ `interactive`/`native_flow` (fork patch) |

See [How the binary button nodes are built](#how-the-binary-button-nodes-are-built)
for what the last column means.

> 🩹 **Fork patch applied (`send_interactive_patch.go`).**
>
> Upstream only attaches the `<biz>` routing node for `ButtonsMessage` and
> `ListMessage`. This fork also emits the **full nested** node for
> `TemplateMessage` and `InteractiveMessage`:
>
> ```xml
> <biz>
>   <interactive type="native_flow" v="1">
>     <native_flow name="..." v="2"/>
>   </interactive>
> </biz>
> ```
>
> The patch is isolated in [`send_interactive_patch.go`](../send_interactive_patch.go)
> with small guarded hooks in [`send.go`](../send.go) (`getButtonTypeFromMessage`,
> `getButtonAttributes`, the `<biz>` block in `getMessageContent`, and a relocate
> hook in `sendDM`).
>
> **`<bot biz_bot="1"/>` node (added).** Matching the reference Baileys
> implementation (rsalcara/InfiniteAPI), for **private 1:1** interactive messages
> the patch now appends a `<bot biz_bot="1"/>` node immediately after `<biz>`, and
> relocates both to the end of the stanza so the order is
> `device-identity → tctoken → biz → bot`. The bot node is what makes **lists and
> CTA-only** buttons render on Web/Desktop. It is injected for quick_reply / CTA /
> list, and **skipped for carousel and catalog**. The inner `<native_flow>` uses
> `v="9"` (not `2`) and a `name` from the reference map (`payment_info`, `mpm`,
> `order_details`, else `mixed`). The emitted `biz`/`bot` subtree is logged at
> INFO for auditing. **Still experimental — see the ban-risk warning above.**
>
> ✅ **Field test result (see [test report](interactive_messages_test_report.md)).**
> With the nested node, `BuildInteractiveMessage` (native flow) **renders as real
> clickable buttons** on a normal recipient and the tap comes back as a response
> event — confirmed end-to-end. The **single-level** node (interactive with no
> `native_flow` child) is rejected by the server with error 479, which is why the
> grandchild is required.
>
> ⚠️ **Caveats from the same test:** the other types are weaker — `ListMessage`
> tends to render as plain text, and `CarouselMessage` may still show "your
> WhatsApp version is not compatible" on some clients. Prefer
> `BuildInteractiveMessage` (native flow) for buttons that must render. And this
> remains **experimental / ban-risk** — test only with disposable accounts.

## Usage

All helpers return a `*waE2E.Message`. Send it like any other message:

```go
ctx := context.Background()
to := types.NewJID("5511999999999", types.DefaultUserServer)

msg := whatsmeow.BuildButtonsMessage("How can we help?", "Pick an option", []whatsmeow.QuickReplyButton{
    {ID: "support", DisplayText: "Talk to support"},
    {ID: "billing", DisplayText: "Billing"},
    {ID: "other", DisplayText: "Something else"},
})
_, err := cli.SendMessage(ctx, to, msg)
```

> 📵 **Recipient JID — handle the Brazilian 9th digit.** `SendMessage` delivers
> to the exact JID it is given and does **not** normalize phone numbers. For some
> accounts the dialed number differs from the registered JID (e.g.
> `5577988272902` with the 9 resolves to `557788272902` without it). Sending to
> the non-canonical JID is accepted by the server (you get a message ID) but
> **delivered to nobody**. Resolve it first:
>
> ```go
> to, err := cli.ResolveRecipientJID(ctx, "5577988272902") // -> 557788272902@s.whatsapp.net
> if err != nil { /* not on WhatsApp */ }
> _, err = cli.SendMessage(ctx, to, msg)
> ```

### Quick-reply buttons (`ButtonsMessage`)

```go
msg := whatsmeow.BuildButtonsMessage(
    "Body text shown above the buttons",
    "Optional footer",            // pass "" to omit
    []whatsmeow.QuickReplyButton{
        {ID: "yes", DisplayText: "Yes"},
        {ID: "no", DisplayText: "No"},
    },
)
```

Up to `whatsmeow.MaxQuickReplyButtons` (3) buttons; extras are dropped to match
WhatsApp's limit.

### Template buttons (`TemplateMessage`)

> 🩹 **Fork patch applies the `<biz>`/native_flow routing node** for this type (see the [fork patch note](#interactive-button-message-helpers) above). Note: emitting the node does not guarantee the recipient renders buttons — WhatsApp gates that to official Business senders.

Mix quick-reply, URL and call buttons. Indexes are assigned automatically.

```go
msg := whatsmeow.BuildTemplateMessage(
    "Your order is ready 🎉",
    "Tap a button below",         // footer, pass "" to omit
    []*waE2E.HydratedTemplateButton{
        whatsmeow.NewQuickReplyTemplateButton("Confirm", "confirm-id"),
        whatsmeow.NewURLTemplateButton("Track order", "https://example.com/track"),
        whatsmeow.NewCallTemplateButton("Call us", "+5511999999999"),
    },
)
```

### List message (`ListMessage`)

```go
msg := whatsmeow.BuildListMessage(
    "Menu",                       // header title
    "Choose a category",          // body description
    "Open menu",                  // button that opens the list
    "Footer text",                // footer, pass "" to omit
    []whatsmeow.ListSection{
        {
            Title: "Drinks",
            Rows: []whatsmeow.ListRow{
                {Title: "Coffee", Description: "Hot and fresh", RowID: "coffee"},
                {Title: "Tea", RowID: "tea"},
            },
        },
    },
)
```

### Native-flow interactive message (`InteractiveMessage`)

> 🩹 **Fork patch applies the `<biz>`/native_flow routing node** for this type (see the [fork patch note](#interactive-button-message-helpers) above). Note: emitting the node does not guarantee the recipient renders buttons — WhatsApp gates that to official Business senders.

This is the modern interactive format. Header is optional (pass `nil`).

```go
header := whatsmeow.NewInteractiveHeaderText("Welcome", "Choose below")
msg := whatsmeow.BuildInteractiveMessage(
    "Body text",
    "Footer text",                // pass "" to omit
    header,                       // or nil for no header
    []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
        whatsmeow.NewQuickReplyNativeFlowButton("Yes", "yes"),
        whatsmeow.NewURLNativeFlowButton("Open site", "https://example.com"),
        whatsmeow.NewCallNativeFlowButton("Call", "+5511999999999"),
    },
)
```

For a media header, upload the media first and build the corresponding
`*waE2E.ImageMessage` / `*waE2E.VideoMessage` / `*waE2E.DocumentMessage`, then:

```go
header := whatsmeow.NewInteractiveHeaderImage(imageMessage, "Title", "Subtitle")
```

### Carousel (`InteractiveMessage` + `CarouselMessage`)

> 🩹 **Fork patch applies the `<biz>`/native_flow routing node** for this type (see the [fork patch note](#interactive-button-message-helpers) above). Note: emitting the node does not guarantee the recipient renders buttons — WhatsApp gates that to official Business senders.

Each card is its own native-flow interactive message, optionally with a media
header. The carousel layout defaults to `HSCROLL_CARDS`; pass an optional last
argument to override it:

```go
msg := whatsmeow.BuildCarouselMessage("Check out our deals", cards,
    waE2E.InteractiveMessage_CarouselMessage_ALBUM_IMAGE.Enum())
```

```go
msg := whatsmeow.BuildCarouselMessage("Check out our deals", []whatsmeow.CarouselCard{
    {
        Header: whatsmeow.NewInteractiveHeaderImage(card1Image, "Plan A", ""),
        Body:   "Our starter plan",
        Footer: "Best for individuals",
        Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
            whatsmeow.NewURLNativeFlowButton("Buy Plan A", "https://example.com/a"),
        },
    },
    {
        Header: whatsmeow.NewInteractiveHeaderImage(card2Image, "Plan B", ""),
        Body:   "Our pro plan",
        Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
            whatsmeow.NewURLNativeFlowButton("Buy Plan B", "https://example.com/b"),
        },
    },
})
```

## How the binary button nodes are built

For an interactive message to render, WhatsApp expects the outgoing stanza to
carry an extra child node alongside the encrypted payload. In this fork that node
is the `<biz>` wrapper, added in [`send.go`](../send.go) inside
`Client.getMessageContent`:

```go
// send.go (~line 1144)
if buttonType := getButtonTypeFromMessage(message); buttonType != "" {
    content = append(content, waBinary.Node{
        Tag: "biz",
        Content: []waBinary.Node{{
            Tag:   buttonType,
            Attrs: getButtonAttributes(message),
        }},
    })
}
```

`getButtonTypeFromMessage` (after unwrapping `ViewOnceMessage`,
`ViewOnceMessageV2` and `EphemeralMessage`) only returns a non-empty type for:

- `ButtonsMessage` → `"buttons"`
- `ListMessage` → `"list"`
- the response types (`ButtonsResponseMessage`, `ListResponseMessage`,
  `InteractiveResponseMessage`)

It does **not** match `TemplateMessage` or `InteractiveMessage`. As a result:

- `BuildButtonsMessage` and `BuildListMessage` get the `<biz>` wrapper
  automatically — they work with the send core unchanged.
- `BuildTemplateMessage`, `BuildInteractiveMessage` and `BuildCarouselMessage`
  are sent as plain encrypted message content with **no** `<biz>` /
  `native_flow` wrapper node. They may not render on all clients.

`getButtonAttributes` already has a `TemplateMessage` branch (returning empty
attrs), but it is currently unreachable because `getButtonTypeFromMessage` never
returns a type for it.

### If template / native-flow / carousel need to render reliably

Making those types render reliably would require a **core change** to the send
flow (explicitly out of scope for this PR, which must not touch the send core).
The minimal change would be to teach `getButtonTypeFromMessage` about the extra
types, e.g.:

```go
// send.go, getButtonTypeFromMessage
case msg.TemplateMessage != nil:
    return "buttons"          // template hydrated buttons ride the buttons node
case msg.InteractiveMessage != nil:
    return "interactive"      // native_flow / carousel
```

and a matching `getButtonAttributes` branch for `InteractiveMessage` (typically
`{"v": "1", "type": "native_flow"}`). This needs validation against a live client
and coordination with the upstream-sync automation before being applied — do not
ship it blind.
