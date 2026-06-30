# Native-flow carousel — field test report

**Date:** 2026-06-30
**Branch:** `feature/native-flow-carousel` (base `main`)
**Sender:** disposable test account (linked device, **non-Business**).
**Recipient:** `557798020125` (canonical JID via `IsOnWhatsApp`; carousel addressed to its LID).

## carouselMessage content (audit)

```
carouselMessage: 2 cards, messageVersion=1
card 0: button name=cta_url     params={"display_text":"Buy A","url":"https://example.com/a","merchant_url":"https://example.com/a"}
card 0: button name=quick_reply params={"display_text":"Pick A","id":"card-a"}
card 1: button name=cta_url     params={"display_text":"Buy B","url":"https://example.com/b","merchant_url":"https://example.com/b"}
card 1: button name=quick_reply params={"display_text":"Pick B","id":"card-b"}
```

Each card carries a header (title + subtitle = card footer + uploaded image),
body, footer and the native-flow buttons above. The top InteractiveMessage has
`header{title,hasMediaAttachment:false}`, `body`, `footer` and
`CarouselMessage{cards, messageVersion:1}`. **Not** wrapped in ViewOnce.

## Emitted stanza (biz/bot audit log)

```
Interactive stanza nodes for 258080709300320@lid:
  biz=<biz>
        <interactive type="native_flow" v="1"><native_flow name="mixed" v="9"/></interactive>
        <quality_control decision_id="02ecc0723754eadc6823255c928f9d9d52ca2c22"><decision_source value="df"/></quality_control>
      </biz>
  bot=<none>
```

Confirms every requirement:
- `<quality_control decision_id="<40 hex = 20 bytes>"><decision_source value="df"/></quality_control>` sits inside `<biz>` next to `interactive>native_flow`.
- `bot=<none>` — the bot node is omitted for carousel.
- `native_flow v="9" name="mixed"`, `interactive v="1"`.
- Envelope addressed to the recipient **LID** (`258080709300320@lid`), not the PN — the carousel LID addressing worked.

## Send-level result

Both messages sent — **no error 400**, zero `SQLITE_BUSY`: Control
`3EB0D80FA0A86B82083728`, Carousel `3EB0D67B5D873525EBF56D`.

## Rendering + response on the recipient device ✅

The recipient saw the carousel with **2 swipeable cards** (Plan A / Plan B, each
with its uploaded image) and clickable **Buy** (cta_url) + **Pick** (quick_reply)
buttons. Tapping **Pick A** produced, on the sender's listening client:

```
↩️  templateButtonReply from <recipient-lid>: selectedID="card-a" displayText="Pick A" index=1
```

So the carousel renders end-to-end and a card-button tap round-trips with the
selected `id` (and card index).

## Conclusion

The native-flow carousel — `InteractiveMessage{CarouselMessage}` (no ViewOnce) +
the `<quality_control>` biz node + omitted bot node + LID envelope addressing —
**renders with swipeable cards and clickable buttons, and the tap is received.**

⚠️ Still **experimental / account-ban risk** — disposable accounts only; never the
production (atendzappy) account. Do not promote without Tech Lead sign-off and an
evaluation of the official WhatsApp Business Cloud API.

## Reproduce

```sh
export TEST_RECIPIENT=557798020125
go run ./examples/interactive-test   # uploads 2 images, logs carouselMessage + biz/bot stanza
# LISTEN_ONLY=1 to capture the card-button response
```
