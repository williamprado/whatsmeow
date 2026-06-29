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

// This file provides convenience constructors for the interactive ("button")
// message types that whatsmeow already supports at the protocol level. The goal
// is to build the correct waE2E.* structs from simple inputs so callers can
// pass the result straight to Client.SendMessage.
//
// Supported families:
//
//   - BuildButtonsMessage:     simple quick-reply buttons (up to 3)  -> waE2E.ButtonsMessage
//   - BuildTemplateMessage:    hydrated template buttons (reply/url/call) -> waE2E.TemplateMessage
//   - BuildListMessage:        single-select list with sections/rows  -> waE2E.ListMessage
//   - BuildInteractiveMessage: a single native-flow button group      -> waE2E.InteractiveMessage
//   - BuildCarouselMessage:    a carousel of interactive cards         -> waE2E.InteractiveMessage
//
// ⚠️ EXPERIMENTAL / ACCOUNT-BAN RISK
//
// Sending interactive messages from non-official libraries is considered
// experimental. WhatsApp actively blocks non-business accounts from sending
// buttons and may ban accounts that do. Only test with disposable accounts in a
// development environment. Do not enable in production without sign-off and an
// evaluation of the official WhatsApp Business API.
//
// Node-wrapping note (see send.go):
//
//   getButtonTypeFromMessage only recognizes ButtonsMessage and ListMessage, so
//   only those two get the <biz> wrapper node added automatically during send.
//   TemplateMessage and InteractiveMessage (native_flow / carousel) are sent as
//   plain encrypted message content with no extra node. These helpers only build
//   the message structs; they do not alter the send core. See PR notes for the
//   exact location if biz/interactive node wrapping needs to be complemented.

// MaxQuickReplyButtons is the maximum number of quick-reply buttons WhatsApp
// accepts in a single ButtonsMessage.
const MaxQuickReplyButtons = 3

// Native-flow button "name" values understood by the WhatsApp clients.
const (
	nativeFlowQuickReply = "quick_reply"
	nativeFlowCTAURL     = "cta_url"
	nativeFlowCTACall    = "cta_call"
)

// interactiveMessageVersion is the messageVersion the clients expect on
// native-flow and carousel messages.
const interactiveMessageVersion int32 = 1

// QuickReplyButton is a single tap-to-reply button for BuildButtonsMessage.
type QuickReplyButton struct {
	// ID is the developer-defined identifier returned in the buttons response.
	ID string
	// DisplayText is the label shown on the button.
	DisplayText string
}

// BuildButtonsMessage builds a *waE2E.Message containing a ButtonsMessage with
// simple quick-reply buttons. body is the message text shown above the buttons
// and footer is optional small print below them. Pass at most MaxQuickReplyButtons
// buttons; additional ones are dropped to match WhatsApp's limit.
//
// ButtonsMessage is one of the two types that the send core wraps in a <biz>
// node automatically, so it does not require any core change to render.
func BuildButtonsMessage(body, footer string, buttons []QuickReplyButton) *waE2E.Message {
	if len(buttons) > MaxQuickReplyButtons {
		buttons = buttons[:MaxQuickReplyButtons]
	}
	protoButtons := make([]*waE2E.ButtonsMessage_Button, len(buttons))
	for i, btn := range buttons {
		protoButtons[i] = &waE2E.ButtonsMessage_Button{
			ButtonID: proto.String(btn.ID),
			ButtonText: &waE2E.ButtonsMessage_Button_ButtonText{
				DisplayText: proto.String(btn.DisplayText),
			},
			Type: waE2E.ButtonsMessage_Button_RESPONSE.Enum(),
		}
	}
	bm := &waE2E.ButtonsMessage{
		ContentText: proto.String(body),
		Buttons:     protoButtons,
		HeaderType:  waE2E.ButtonsMessage_EMPTY.Enum(),
	}
	if footer != "" {
		bm.FooterText = proto.String(footer)
	}
	return &waE2E.Message{ButtonsMessage: bm}
}

// NewQuickReplyTemplateButton builds a hydrated quick-reply template button.
// The index is assigned by BuildTemplateMessage if left at zero.
func NewQuickReplyTemplateButton(displayText, id string) *waE2E.HydratedTemplateButton {
	return &waE2E.HydratedTemplateButton{
		HydratedButton: &waE2E.HydratedTemplateButton_QuickReplyButton{
			QuickReplyButton: &waE2E.HydratedTemplateButton_HydratedQuickReplyButton{
				DisplayText: proto.String(displayText),
				ID:          proto.String(id),
			},
		},
	}
}

// NewURLTemplateButton builds a hydrated URL template button that opens a link.
func NewURLTemplateButton(displayText, url string) *waE2E.HydratedTemplateButton {
	return &waE2E.HydratedTemplateButton{
		HydratedButton: &waE2E.HydratedTemplateButton_UrlButton{
			UrlButton: &waE2E.HydratedTemplateButton_HydratedURLButton{
				DisplayText: proto.String(displayText),
				URL:         proto.String(url),
			},
		},
	}
}

// NewCallTemplateButton builds a hydrated call template button that dials a
// phone number.
func NewCallTemplateButton(displayText, phoneNumber string) *waE2E.HydratedTemplateButton {
	return &waE2E.HydratedTemplateButton{
		HydratedButton: &waE2E.HydratedTemplateButton_CallButton{
			CallButton: &waE2E.HydratedTemplateButton_HydratedCallButton{
				DisplayText: proto.String(displayText),
				PhoneNumber: proto.String(phoneNumber),
			},
		},
	}
}

// BuildTemplateMessage builds a *waE2E.Message containing a TemplateMessage with
// a hydrated four-row template. content is the body text, footer is optional
// small print, and buttons may mix quick-reply, URL and call buttons (use the
// NewXxxTemplateButton constructors). Any button whose Index is still zero is
// assigned a 1-based index in order.
func BuildTemplateMessage(content, footer string, buttons []*waE2E.HydratedTemplateButton) *waE2E.Message {
	for i, btn := range buttons {
		if btn.GetIndex() == 0 {
			btn.Index = proto.Uint32(uint32(i + 1))
		}
	}
	tmpl := &waE2E.TemplateMessage_HydratedFourRowTemplate{
		HydratedContentText: proto.String(content),
		HydratedButtons:     buttons,
	}
	if footer != "" {
		tmpl.HydratedFooterText = proto.String(footer)
	}
	return &waE2E.Message{
		TemplateMessage: &waE2E.TemplateMessage{
			Format: &waE2E.TemplateMessage_HydratedFourRowTemplate_{
				HydratedFourRowTemplate: tmpl,
			},
			HydratedTemplate: tmpl,
		},
	}
}

// ListRow is a single selectable row inside a list section.
type ListRow struct {
	Title       string
	Description string
	// RowID is the identifier returned in the list response when this row is picked.
	RowID string
}

// ListSection groups rows under a heading in a list message.
type ListSection struct {
	Title string
	Rows  []ListRow
}

// BuildListMessage builds a *waE2E.Message containing a single-select
// ListMessage. title is the header title, description is the body text,
// buttonText is the label of the button that opens the list, footer is optional
// small print, and sections holds the selectable rows.
//
// ListMessage is one of the two types that the send core wraps in a <biz> node
// automatically, so it does not require any core change to render.
func BuildListMessage(title, description, buttonText, footer string, sections []ListSection) *waE2E.Message {
	protoSections := make([]*waE2E.ListMessage_Section, len(sections))
	for i, section := range sections {
		rows := make([]*waE2E.ListMessage_Row, len(section.Rows))
		for j, row := range section.Rows {
			protoRow := &waE2E.ListMessage_Row{
				Title: proto.String(row.Title),
				RowID: proto.String(row.RowID),
			}
			if row.Description != "" {
				protoRow.Description = proto.String(row.Description)
			}
			rows[j] = protoRow
		}
		protoSections[i] = &waE2E.ListMessage_Section{
			Title: proto.String(section.Title),
			Rows:  rows,
		}
	}
	lm := &waE2E.ListMessage{
		Title:       proto.String(title),
		Description: proto.String(description),
		ButtonText:  proto.String(buttonText),
		ListType:    waE2E.ListMessage_SINGLE_SELECT.Enum(),
		Sections:    protoSections,
	}
	if footer != "" {
		lm.FooterText = proto.String(footer)
	}
	return &waE2E.Message{ListMessage: lm}
}

// NativeFlowButton is a single button inside a native-flow interactive message.
// Name is the button kind (e.g. "quick_reply", "cta_url", "cta_call") and
// ParamsJSON is the raw JSON payload the client expects for that kind. Prefer the
// NewQuickReplyNativeFlowButton / NewURLNativeFlowButton / NewCallNativeFlowButton
// constructors, which fill these in for you.
type nativeFlowButtonParams struct {
	DisplayText string `json:"display_text"`
	ID          string `json:"id,omitempty"`
	URL         string `json:"url,omitempty"`
	MerchantURL string `json:"merchant_url,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
}

func newNativeFlowButton(name string, params nativeFlowButtonParams) *waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton {
	// Marshalling a fixed struct of strings cannot fail; ignore the error.
	raw, _ := json.Marshal(params)
	return &waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
		Name:             proto.String(name),
		ButtonParamsJSON: proto.String(string(raw)),
	}
}

// NewQuickReplyNativeFlowButton builds a tap-to-reply native-flow button.
func NewQuickReplyNativeFlowButton(displayText, id string) *waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton {
	return newNativeFlowButton(nativeFlowQuickReply, nativeFlowButtonParams{DisplayText: displayText, ID: id})
}

// NewURLNativeFlowButton builds a native-flow button that opens a URL.
func NewURLNativeFlowButton(displayText, url string) *waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton {
	return newNativeFlowButton(nativeFlowCTAURL, nativeFlowButtonParams{DisplayText: displayText, URL: url, MerchantURL: url})
}

// NewCallNativeFlowButton builds a native-flow button that dials a phone number.
func NewCallNativeFlowButton(displayText, phoneNumber string) *waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton {
	return newNativeFlowButton(nativeFlowCTACall, nativeFlowButtonParams{DisplayText: displayText, PhoneNumber: phoneNumber})
}

// NewInteractiveHeaderText builds a text-only interactive header.
func NewInteractiveHeaderText(title, subtitle string) *waE2E.InteractiveMessage_Header {
	return &waE2E.InteractiveMessage_Header{
		Title:              proto.String(title),
		Subtitle:           proto.String(subtitle),
		HasMediaAttachment: proto.Bool(false),
	}
}

// NewInteractiveHeaderImage builds an interactive header with an image. The
// image must already be uploaded (use Client.Upload + Client.BuildImageMessage or
// build the *waE2E.ImageMessage yourself).
func NewInteractiveHeaderImage(image *waE2E.ImageMessage, title, subtitle string) *waE2E.InteractiveMessage_Header {
	return &waE2E.InteractiveMessage_Header{
		Media:              &waE2E.InteractiveMessage_Header_ImageMessage{ImageMessage: image},
		Title:              proto.String(title),
		Subtitle:           proto.String(subtitle),
		HasMediaAttachment: proto.Bool(true),
	}
}

// NewInteractiveHeaderVideo builds an interactive header with a video. The video
// must already be uploaded.
func NewInteractiveHeaderVideo(video *waE2E.VideoMessage, title, subtitle string) *waE2E.InteractiveMessage_Header {
	return &waE2E.InteractiveMessage_Header{
		Media:              &waE2E.InteractiveMessage_Header_VideoMessage{VideoMessage: video},
		Title:              proto.String(title),
		Subtitle:           proto.String(subtitle),
		HasMediaAttachment: proto.Bool(true),
	}
}

// NewInteractiveHeaderDocument builds an interactive header with a document. The
// document must already be uploaded.
func NewInteractiveHeaderDocument(document *waE2E.DocumentMessage, title, subtitle string) *waE2E.InteractiveMessage_Header {
	return &waE2E.InteractiveMessage_Header{
		Media:              &waE2E.InteractiveMessage_Header_DocumentMessage{DocumentMessage: document},
		Title:              proto.String(title),
		Subtitle:           proto.String(subtitle),
		HasMediaAttachment: proto.Bool(true),
	}
}

// BuildInteractiveMessage builds a *waE2E.Message containing a single
// native-flow InteractiveMessage. body is the message text, footer is optional
// small print, header is an optional header (use the NewInteractiveHeaderXxx
// constructors, or nil for none) and buttons holds the native-flow buttons
// (use the NewXxxNativeFlowButton constructors).
//
// Note: InteractiveMessage is not wrapped in a <biz>/native_flow node by the
// current send core. See the file-level comment and PR notes.
func BuildInteractiveMessage(body, footer string, header *waE2E.InteractiveMessage_Header, buttons []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton) *waE2E.Message {
	return &waE2E.Message{
		InteractiveMessage: newInteractiveMessage(body, footer, header, buttons),
	}
}

func newInteractiveMessage(body, footer string, header *waE2E.InteractiveMessage_Header, buttons []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton) *waE2E.InteractiveMessage {
	msg := &waE2E.InteractiveMessage{
		Header: header,
		Body:   &waE2E.InteractiveMessage_Body{Text: proto.String(body)},
		InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
			NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
				Buttons:        buttons,
				MessageVersion: proto.Int32(interactiveMessageVersion),
			},
		},
	}
	if footer != "" {
		msg.Footer = &waE2E.InteractiveMessage_Footer{Text: proto.String(footer)}
	}
	return msg
}

// CarouselCard is a single card in a carousel. Header is an optional media/text
// header, Body is the card text, Footer is optional small print and Buttons are
// the card's native-flow buttons.
type CarouselCard struct {
	Header  *waE2E.InteractiveMessage_Header
	Body    string
	Footer  string
	Buttons []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton
}

// BuildCarouselMessage builds a *waE2E.Message containing a carousel of
// interactive cards. body is the introductory text shown above the carousel and
// cards holds the individual cards (each rendered as a native-flow
// InteractiveMessage).
//
// Note: InteractiveMessage is not wrapped in a <biz>/native_flow node by the
// current send core. See the file-level comment and PR notes.
func BuildCarouselMessage(body string, cards []CarouselCard) *waE2E.Message {
	protoCards := make([]*waE2E.InteractiveMessage, len(cards))
	for i, card := range cards {
		protoCards[i] = newInteractiveMessage(card.Body, card.Footer, card.Header, card.Buttons)
	}
	return &waE2E.Message{
		InteractiveMessage: &waE2E.InteractiveMessage{
			Body: &waE2E.InteractiveMessage_Body{Text: proto.String(body)},
			InteractiveMessage: &waE2E.InteractiveMessage_CarouselMessage_{
				CarouselMessage: &waE2E.InteractiveMessage_CarouselMessage{
					Cards:          protoCards,
					MessageVersion: proto.Int32(interactiveMessageVersion),
				},
			},
		},
	}
}
