// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// ============================================================================
// CUSTOM FORK PATCH — native-flow carousel (renderable format)
// ============================================================================
//
// Replicates the reference generateCarouselMessage (rsalcara/InfiniteAPI,
// src/Utils/messages.ts): an InteractiveMessage whose InteractiveMessage oneof is
// a CarouselMessage{ cards, messageVersion: 1 }, with a top-level header{title},
// body{text} and optional footer. Unlike the list, the carousel is sent directly
// as the root InteractiveMessage — it is NOT wrapped in ViewOnce.
//
// Each card is an InteractiveMessage with a header (title + subtitle = the card
// footer, plus an optional image/video), a body, an optional footer and a
// nativeFlowMessage with the card's buttons.
//
// The <biz> routing node (interactive>native_flow v="9" name="mixed") plus the
// carousel-only <quality_control> node, the omission of the <bot> node, and the
// LID envelope addressing are all handled by send_interactive_patch.go.
//
// ⚠️ EXPERIMENTAL — account-ban risk. Disposable accounts only.

// Carousel size limits enforced by WhatsApp (mirrors the reference).
const (
	MinCarouselCards = 2
	MaxCarouselCards = 10
)

// CarouselCard is one card in a carousel.
//
//   - Title:   card header title.
//   - Body:    card body text.
//   - Footer:  optional card footer (also used as the header subtitle).
//   - Image:   optional header image (mutually exclusive with Video). Must be an
//     already-uploaded *waE2E.ImageMessage.
//   - Video:   optional header video (mutually exclusive with Image).
//   - Buttons: the card's native-flow buttons (at least one). Use the
//     NewURLNativeFlowButton / NewQuickReplyNativeFlowButton /
//     NewCopyNativeFlowButton / NewCallNativeFlowButton constructors.
type CarouselCard struct {
	Title   string
	Body    string
	Footer  string
	Image   *waE2E.ImageMessage
	Video   *waE2E.VideoMessage
	Buttons []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton
}

// BuildCarouselMessage builds a *waE2E.Message carousel, replicating the
// reference structure. title/body/footer are the top-level header title, body
// text and optional footer; cards holds the individual cards.
//
// Validation (returns an error): between MinCarouselCards and MaxCarouselCards
// cards, each card must have at least one button, and a card may not set both
// Image and Video.
func BuildCarouselMessage(title, body, footer string, cards []CarouselCard) (*waE2E.Message, error) {
	if len(cards) < MinCarouselCards {
		return nil, fmt.Errorf("carousel: %d cards, need at least %d", len(cards), MinCarouselCards)
	}
	if len(cards) > MaxCarouselCards {
		return nil, fmt.Errorf("carousel: %d cards exceeds the max of %d", len(cards), MaxCarouselCards)
	}

	protoCards := make([]*waE2E.InteractiveMessage, len(cards))
	for i, card := range cards {
		if len(card.Buttons) == 0 {
			return nil, fmt.Errorf("carousel: card %d (%q) has no buttons", i, card.Title)
		}
		if card.Image != nil && card.Video != nil {
			return nil, fmt.Errorf("carousel: card %d (%q) cannot have both image and video", i, card.Title)
		}
		protoCards[i] = buildCarouselCard(card)
	}

	top := &waE2E.InteractiveMessage{
		Header: &waE2E.InteractiveMessage_Header{
			Title:              proto.String(title),
			HasMediaAttachment: proto.Bool(false),
		},
		Body: &waE2E.InteractiveMessage_Body{Text: proto.String(body)},
		InteractiveMessage: &waE2E.InteractiveMessage_CarouselMessage_{
			CarouselMessage: &waE2E.InteractiveMessage_CarouselMessage{
				Cards:          protoCards,
				MessageVersion: proto.Int32(1),
			},
		},
	}
	if footer != "" {
		top.Footer = &waE2E.InteractiveMessage_Footer{Text: proto.String(footer)}
	}

	// NOT wrapped in ViewOnce — the carousel goes directly as the root
	// InteractiveMessage.
	return &waE2E.Message{InteractiveMessage: top}, nil
}

func buildCarouselCard(card CarouselCard) *waE2E.InteractiveMessage {
	hasMedia := card.Image != nil || card.Video != nil
	header := &waE2E.InteractiveMessage_Header{
		Title:              proto.String(card.Title),
		Subtitle:           proto.String(card.Footer), // subtitle = card footer (reference)
		HasMediaAttachment: proto.Bool(hasMedia),
	}
	switch {
	case card.Image != nil:
		header.Media = &waE2E.InteractiveMessage_Header_ImageMessage{ImageMessage: card.Image}
	case card.Video != nil:
		header.Media = &waE2E.InteractiveMessage_Header_VideoMessage{VideoMessage: card.Video}
	}

	cardMsg := &waE2E.InteractiveMessage{
		Header: header,
		Body:   &waE2E.InteractiveMessage_Body{Text: proto.String(card.Body)},
		InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
			NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
				Buttons: card.Buttons,
			},
		},
	}
	if card.Footer != "" {
		cardMsg.Footer = &waE2E.InteractiveMessage_Footer{Text: proto.String(card.Footer)}
	}
	return cardMsg
}
