# Pix / CTA buttons — field test report

**Date:** 2026-06-30
**Branch:** `feature/pix-and-cta-buttons` (base `main`)
**Sender:** disposable test account (linked device, **non-Business**).
**Recipient:** `557798020125`.

## buttonParamsJSON (audit)

```
Pix copy-key (cta_copy):
  cta_copy    {"display_text":"Copiar chave Pix","copy_code":"pix@kesassessoria.com.br"}
  quick_reply {"display_text":"Já paguei","id":"pix-paid"}

Pix payment (review_and_pay):
  review_and_pay {"currency":"BRL","total_amount":{"value":1000,"offset":100},"reference_id":"test-order-1",
                  "type":"physical-goods","payment_settings":[{"type":"pix_static_code",
                  "pix_static_code":{"merchant_name":"Kes Assessoria","key":"pix@kesassessoria.com.br","key_type":"EMAIL"}}],
                  "share_payment_status":false}
  cta_url        {"display_text":"Abrir cobrança","url":"https://example.com/pix/charge","merchant_url":"…"}

Carousel (cta_call + cta_copy cards):
  cta_call    {"display_text":"Ligar agora","phone_number":"+5577998020125"}
  quick_reply {"display_text":"Prefiro chat","id":"call-chat"}
  cta_copy    {"display_text":"Copiar chave","copy_code":"pix@kesassessoria.com.br"}
  quick_reply {"display_text":"Já paguei","id":"carousel-paid"}
```

## Emitted stanza (biz/bot audit log)

```
Pix copy-key:   biz=<biz><interactive type="native_flow" v="1"><native_flow name="mixed"        v="9"/></interactive></biz> bot=<bot biz_bot="1"/>
Pix payment:    biz=<biz><interactive type="native_flow" v="1"><native_flow name="payment_info" v="9"/></interactive></biz> bot=<bot biz_bot="1"/>
CTA carousel:   biz=<biz><interactive…/><quality_control decision_id="25fa73ad…"><decision_source value="df"/></quality_control></biz> bot=<none>  (to @lid)
```

The `review_and_pay` button correctly routes to the **`payment_info`** native_flow
name on the biz node (SPECIAL_FLOW_NAMES), confirmed live.

## Send-level result

All sent — **no error 400**, zero `SQLITE_BUSY`: Pix copy-key
`3EB05E0F4EED0100C258F7`, Pix payment `3EB0CD895A60A44DDAA7AD`, CTA carousel
`3EB00F88B7F32330CAD205`.

## Rendering + response on the recipient device

| Message / button | Rendered? |
| --- | --- |
| **Pix copy-key (`cta_copy`)** | ✅ renders with a copy icon ("Copiar chave Pix"), clickable |
| **`cta_call` "Ligar agora"** (carousel card) | ✅ renders with a phone icon, clickable |
| **`cta_copy` "Copiar chave"** (carousel card) | ✅ renders, swipeable |
| quick_reply buttons | ✅ taps captured: `pix-paid`, `call-chat`, `carousel-paid` |
| **Pix payment (`review_and_pay`)** | ❌ **did NOT render** on the normal account — the message is absent from the thread (server accepted it with an ID, recipient dropped it) |
| `cta_url` "Abrir cobrança" (fallback) | ❌ not shown — it lived inside the dropped review_and_pay message |

Captured responses (taps round-trip as `templateButtonReply`):

```
selectedID="pix-paid"      (Pix copy-key message)
selectedID="call-chat"     (carousel cta_call card)
selectedID="carousel-paid" (carousel cta_copy card)
```

## Conclusion

- ✅ **`NewPixCopyKeyButton` (cta_copy)** and the **`cta_call` / `cta_copy`** CTA
  buttons render and work end-to-end, including inside carousel cards.
- ❌ **`NewPixPaymentButton` (review_and_pay / payment_info) does NOT render on a
  normal (non-Business) account** — the message is silently dropped. This matches
  the expectation: native WhatsApp payments are gated to Business accounts with
  payments enabled. The routing (payment_info) is correct, but rendering is
  server-gated.
- **Recommendation for Pix:** use **`NewPixCopyKeyButton`** ("Pix copia e cola"
  copy) and/or **`NewPixPaymentLinkButton`** (cta_url to a payment-provider charge
  link). Treat `NewPixPaymentButton` as experimental / Business-only.

⚠️ Still **experimental / account-ban risk** — disposable accounts only; never the
production (atendzappy) account. Do not promote without Tech Lead sign-off and an
evaluation of the official WhatsApp Business Cloud API.

## Reproduce

```sh
export TEST_RECIPIENT=557798020125
go run ./examples/interactive-test   # logs stanza + buttonParamsJSON
# LISTEN_ONLY=1 to capture button responses
```
