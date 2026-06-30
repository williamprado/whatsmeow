# Carousel media (top header + video card) — field test report

**Date:** 2026-06-30
**Branch:** `feature/carousel-media` (base `main`)
**Sender:** disposable test account (linked device, **non-Business**).
**Recipient:** `557798020125` (carousel addressed to its LID).

## carouselMessage content (audit)

```
Carousel A (top media header + 2 image cards): topHeaderMedia=image 800x300 thumb=954B, 2 cards, messageVersion=1
  card 0: media=image 600x400 thumb=1195B   buttons: cta_url + quick_reply
  card 1: media=image 600x400 thumb=1194B   buttons: cta_url + quick_reply
Carousel B (video card + image card): topHeaderMedia=none, 2 cards, messageVersion=1
  card 0: media=video 320x240 3s thumb=8182B  buttons: cta_url + quick_reply
  card 1: media=image 600x400 thumb=1194B     buttons: cta_url + quick_reply
```

The top media header carries an 800x300 image with a generated JPEG thumbnail; the
video card carries a 320x240 / 3s video with an 8 KB JPEG poster frame. Images use
`Client.UploadCarouselImage` (auto dimensions + thumbnail); the video uses
`Client.UploadCarouselVideo` with an ffmpeg-generated thumbnail/dimensions.

## Emitted stanza (both carousels, biz/bot audit log)

```
Interactive stanza nodes for 258080709300320@lid:
  biz=<biz>
        <interactive type="native_flow" v="1"><native_flow name="mixed" v="9"/></interactive>
        <quality_control decision_id="bcad27b6…"><decision_source value="df"/></quality_control>
      </biz>
  bot=<none>
```

Unchanged from the base carousel (the top/card media live inside the encrypted
InteractiveMessage): `quality_control` present, `bot=<none>`, addressed to LID.

## Send-level result

Both carousels sent — **no error 400**, zero `SQLITE_BUSY`, ffmpeg generated the
test video + thumbnail: Carousel A `3EB0E5E7EE054E48FB4B9C`, Carousel B
`3EB03CBBEE1C00D26E8906`.

## Rendering + response on the recipient device ✅

- **Carousel A:** a **media header (image) renders at the top**, above the cards.
- **Carousel B:** the **video card renders inline** with a play button and the
  `0:03` duration badge, and swipes to the image card; **Buy** (cta_url) + **Pick**
  (quick_reply) buttons are clickable.
- Tapping **Pick** on the video card produced:

  ```
  ↩️  templateButtonReply from <recipient-lid>: selectedID="card-v" displayText="Pick" index=1
  ```

So both the top media header and the video card render end-to-end, and a card-
button tap round-trips.

## Conclusion

Both follow-ups work: an optional **top media header** on the carousel, and full
**video card** support (upload + thumbnail/dimensions). Images get dimensions + a
JPEG thumbnail automatically; videos take a caller/ffmpeg-provided thumbnail.

⚠️ Still **experimental / account-ban risk** — disposable accounts only; never the
production (atendzappy) account. Do not promote without Tech Lead sign-off and an
evaluation of the official WhatsApp Business Cloud API.

## Reproduce

```sh
export TEST_RECIPIENT=557798020125
go run ./examples/interactive-test   # needs ffmpeg on PATH for the video card
# LISTEN_ONLY=1 to capture the card-button response
```
