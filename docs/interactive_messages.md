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

| Helper | Builds | Wrapped in `<biz>` node by send core? |
| --- | --- | --- |
| `BuildButtonsMessage` | `waE2E.ButtonsMessage` (up to 3 quick-reply buttons) | ✅ yes (`buttons`) |
| `BuildListMessage` | `waE2E.ListMessage` (single-select sections/rows) | ✅ yes (`list`) |
| `BuildTemplateMessage` | `waE2E.TemplateMessage` (hydrated reply/url/call buttons) | ❌ no |
| `BuildInteractiveMessage` | `waE2E.InteractiveMessage` (single native-flow group) | ❌ no |
| `BuildCarouselMessage` | `waE2E.InteractiveMessage` with `CarouselMessage` | ❌ no |

See [How the binary button nodes are built](#how-the-binary-button-nodes-are-built)
for what the last column means.

> 🚧 **Core limitation — read before using.**
>
> Only **`BuildButtonsMessage`** and **`BuildListMessage`** work out of the box:
> the send core wraps them in the required `<biz>` node automatically, so the
> buttons render on the recipient's device.
>
> **`BuildTemplateMessage`, `BuildInteractiveMessage` and `BuildCarouselMessage`
> are inert** — they build valid structs, but the current send core sends them
> with **no** `<biz>` / `native_flow` node, so the recipient sees no buttons.
> They stay inert until a **core patch in [`send.go`](../send.go)** is applied
> **and validated against a real client**:
>
> 1. register the `TemplateMessage` / `InteractiveMessage` cases in
>    `getButtonTypeFromMessage`, and
> 2. add the matching `native_flow` attributes in `getButtonAttributes`.
>
> The exact patch is in
> [If template / native-flow / carousel need to render reliably](#if-template--native-flow--carousel-need-to-render-reliably).

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

> 🚧 **Inert until the core patch** — see the [core limitation](#interactive-button-message-helpers) above. The struct is built correctly but no buttons render without the `send.go` change.

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

> 🚧 **Inert until the core patch** — see the [core limitation](#interactive-button-message-helpers) above. The struct is built correctly but no buttons render without the `send.go` change.

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

> 🚧 **Inert until the core patch** — see the [core limitation](#interactive-button-message-helpers) above. The struct is built correctly but no buttons render without the `send.go` change.

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
