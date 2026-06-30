// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	"encoding/json"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// ============================================================================
// CUSTOM FORK PATCH — Pix buttons
// ============================================================================
//
// Pix payment buttons built on top of the native-flow button helpers. Two
// flavours:
//
//   - NewPixCopyKeyButton: a cta_copy button whose copy_code is the seller's Pix
//     key (or a full "Pix copia e cola" BR Code). Renders like any cta_copy.
//   - NewPixPaymentButton: the native "review_and_pay" / payment_info flow. This
//     maps to the payment_info native_flow name (SPECIAL_FLOW_NAMES) and carries
//     a payment payload. ⚠️ EXPERIMENTAL: native payments generally require a
//     WhatsApp Business account with payments enabled and may NOT render on a
//     normal account — test and rely on the copy-key / URL fallbacks if so.
//   - NewPixPaymentLinkButton: a cta_url fallback pointing at a Pix charge link.
//
// ⚠️ EXPERIMENTAL — account-ban risk. Disposable accounts only.

// nativeFlowReviewAndPay is the native-flow button name for the review-and-pay
// (payment_info) flow. nativeFlowName maps it to "payment_info" on the biz node.
const nativeFlowReviewAndPay = "review_and_pay"

// NewPixCopyKeyButton builds a cta_copy button that copies the seller's Pix key
// (or a full "Pix copia e cola" BR Code) to the clipboard. This is the most
// reliable Pix button on a non-Business account.
func NewPixCopyKeyButton(displayText, pixKey string) *waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton {
	return NewCopyNativeFlowButton(displayText, pixKey)
}

// NewPixPaymentLinkButton builds a cta_url button pointing at a Pix charge link
// (e.g. a payment-provider checkout URL). Use this as a fallback when the native
// payment button does not render.
func NewPixPaymentLinkButton(displayText, chargeURL string) *waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton {
	return NewURLNativeFlowButton(displayText, chargeURL)
}

// PixPayment configures NewPixPaymentButton (the native review_and_pay flow).
type PixPayment struct {
	// DisplayText is the button label (e.g. "Pagar com Pix").
	DisplayText string
	// AmountCents is the amount in the currency's minor units (e.g. 1000 = R$10,00).
	AmountCents int64
	// Currency defaults to "BRL".
	Currency string
	// PixKey is the receiver's Pix key.
	PixKey string
	// KeyType is the Pix key type (CPF, CNPJ, EMAIL, PHONE, EVP). Optional.
	KeyType string
	// MerchantName is the receiver/merchant display name. Optional.
	MerchantName string
	// ReferenceID is an order/charge reference. Optional.
	ReferenceID string
	// CopyPaste is the full "Pix copia e cola" BR Code. Optional.
	CopyPaste string
}

// pixMoney is WhatsApp's money shape: value in minor units with an offset.
type pixMoney struct {
	Value  int64 `json:"value"`
	Offset int   `json:"offset"`
}

type pixStaticCode struct {
	MerchantName string `json:"merchant_name,omitempty"`
	Key          string `json:"key,omitempty"`
	KeyType      string `json:"key_type,omitempty"`
	Code         string `json:"code,omitempty"`
}

type pixPaymentSetting struct {
	Type          string         `json:"type"`
	PixStaticCode *pixStaticCode `json:"pix_static_code,omitempty"`
}

// pixPaymentParams is the buttonParamsJson for the review_and_pay flow. The exact
// schema accepted by WhatsApp is not publicly documented and is account-gated;
// this is a best-effort payload (see the EXPERIMENTAL note above).
type pixPaymentParams struct {
	Currency           string              `json:"currency"`
	TotalAmount        pixMoney            `json:"total_amount"`
	ReferenceID        string              `json:"reference_id,omitempty"`
	Type               string              `json:"type"`
	PaymentSettings    []pixPaymentSetting `json:"payment_settings"`
	SharePaymentStatus bool                `json:"share_payment_status"`
}

// NewPixPaymentButton builds the native "review_and_pay" / payment_info Pix
// button. ⚠️ EXPERIMENTAL: this typically requires a WhatsApp Business account
// with payments enabled and may not render on a normal account. Prefer
// NewPixCopyKeyButton / NewPixPaymentLinkButton when in doubt.
func NewPixPaymentButton(p PixPayment) *waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton {
	currency := p.Currency
	if currency == "" {
		currency = "BRL"
	}
	params := pixPaymentParams{
		Currency:    currency,
		TotalAmount: pixMoney{Value: p.AmountCents, Offset: 100},
		ReferenceID: p.ReferenceID,
		Type:        "physical-goods",
		PaymentSettings: []pixPaymentSetting{{
			Type: "pix_static_code",
			PixStaticCode: &pixStaticCode{
				MerchantName: p.MerchantName,
				Key:          p.PixKey,
				KeyType:      p.KeyType,
				Code:         p.CopyPaste,
			},
		}},
		SharePaymentStatus: false,
	}
	raw, _ := json.Marshal(params)
	return &waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
		Name:             proto.String(nativeFlowReviewAndPay),
		ButtonParamsJSON: proto.String(string(raw)),
	}
}
