// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	"encoding/json"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

func TestNewPixCopyKeyButton(t *testing.T) {
	btn := NewPixCopyKeyButton("Copiar chave Pix", "pix@example.com")
	if btn.GetName() != nativeFlowCTACopy {
		t.Errorf("name = %q, want cta_copy", btn.GetName())
	}
	var p map[string]string
	if err := json.Unmarshal([]byte(btn.GetButtonParamsJSON()), &p); err != nil {
		t.Fatalf("params not JSON: %v", err)
	}
	if p["display_text"] != "Copiar chave Pix" || p["copy_code"] != "pix@example.com" {
		t.Errorf("params = %v", p)
	}
}

func TestNewPixPaymentLinkButton(t *testing.T) {
	btn := NewPixPaymentLinkButton("Pagar", "https://pay.example.com/abc")
	if btn.GetName() != nativeFlowCTAURL {
		t.Errorf("name = %q, want cta_url", btn.GetName())
	}
	var p map[string]string
	_ = json.Unmarshal([]byte(btn.GetButtonParamsJSON()), &p)
	if p["url"] != "https://pay.example.com/abc" {
		t.Errorf("url = %q", p["url"])
	}
}

func TestNewPixPaymentButton(t *testing.T) {
	btn := NewPixPaymentButton(PixPayment{
		DisplayText:  "Pagar com Pix",
		AmountCents:  1000, // R$10,00
		PixKey:       "pix@example.com",
		KeyType:      "EMAIL",
		MerchantName: "Loja Teste",
		ReferenceID:  "order-1",
		CopyPaste:    "0002012636...6304ABCD",
	})
	if btn.GetName() != nativeFlowReviewAndPay {
		t.Errorf("name = %q, want review_and_pay", btn.GetName())
	}
	var p pixPaymentParams
	if err := json.Unmarshal([]byte(btn.GetButtonParamsJSON()), &p); err != nil {
		t.Fatalf("params not JSON: %v", err)
	}
	if p.Currency != "BRL" {
		t.Errorf("currency = %q, want BRL (default)", p.Currency)
	}
	if p.TotalAmount.Value != 1000 || p.TotalAmount.Offset != 100 {
		t.Errorf("total_amount = %+v, want {1000,100}", p.TotalAmount)
	}
	if len(p.PaymentSettings) != 1 || p.PaymentSettings[0].PixStaticCode == nil {
		t.Fatalf("payment_settings = %+v", p.PaymentSettings)
	}
	sc := p.PaymentSettings[0].PixStaticCode
	if sc.Key != "pix@example.com" || sc.KeyType != "EMAIL" || sc.MerchantName != "Loja Teste" {
		t.Errorf("pix_static_code = %+v", sc)
	}
}

// A message whose native-flow button is review_and_pay must route under the
// payment_info name on the biz node (SPECIAL_FLOW_NAMES).
func TestPixPaymentRoutesAsPaymentInfo(t *testing.T) {
	msg := BuildInteractiveMessage("Pague seu pedido", "", nil,
		[]*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
			NewPixPaymentButton(PixPayment{DisplayText: "Pagar", AmountCents: 500, PixKey: "k"}),
		})
	node, ok := customInteractiveBizNode(msg)
	if !ok {
		t.Fatal("customInteractiveBizNode returned ok=false")
	}
	nf := node.GetChildren()[0].GetChildren()[0]
	if nf.Tag != "native_flow" {
		t.Fatalf("expected native_flow node, got %q", nf.Tag)
	}
	if nf.Attrs["name"] != "payment_info" {
		t.Errorf("native_flow name = %v, want payment_info", nf.Attrs["name"])
	}
}

// cta_call / cta_copy buttons work inside carousel cards.
func TestCarouselCardWithCallAndCopyButtons(t *testing.T) {
	msg, err := BuildCarouselMessage("t", "b", "", []CarouselCard{
		{Title: "Card 1", Body: "b1", Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
			NewCallNativeFlowButton("Ligar", "+5511999999999"),
		}},
		{Title: "Card 2", Body: "b2", Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
			NewPixCopyKeyButton("Copiar Pix", "pix@example.com"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cards := msg.GetInteractiveMessage().GetCarouselMessage().GetCards()
	if got := cards[0].GetNativeFlowMessage().GetButtons()[0].GetName(); got != nativeFlowCTACall {
		t.Errorf("card0 button = %q, want cta_call", got)
	}
	if got := cards[1].GetNativeFlowMessage().GetButtons()[0].GetName(); got != nativeFlowCTACopy {
		t.Errorf("card1 button = %q, want cta_copy", got)
	}
}
