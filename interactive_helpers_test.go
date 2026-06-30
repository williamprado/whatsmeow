// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

func TestBuildButtonsMessage(t *testing.T) {
	msg := BuildButtonsMessage("Pick one", "footer", []QuickReplyButton{
		{ID: "a", DisplayText: "Option A"},
		{ID: "b", DisplayText: "Option B"},
	})
	bm := msg.GetButtonsMessage()
	if bm == nil {
		t.Fatal("ButtonsMessage is nil")
	}
	if bm.GetContentText() != "Pick one" {
		t.Errorf("ContentText = %q, want %q", bm.GetContentText(), "Pick one")
	}
	if bm.GetFooterText() != "footer" {
		t.Errorf("FooterText = %q, want %q", bm.GetFooterText(), "footer")
	}
	if bm.GetHeaderType() != waE2E.ButtonsMessage_EMPTY {
		t.Errorf("HeaderType = %v, want EMPTY", bm.GetHeaderType())
	}
	if len(bm.GetButtons()) != 2 {
		t.Fatalf("got %d buttons, want 2", len(bm.GetButtons()))
	}
	first := bm.GetButtons()[0]
	if first.GetButtonID() != "a" {
		t.Errorf("button[0].ButtonID = %q, want %q", first.GetButtonID(), "a")
	}
	if first.GetButtonText().GetDisplayText() != "Option A" {
		t.Errorf("button[0].DisplayText = %q, want %q", first.GetButtonText().GetDisplayText(), "Option A")
	}
	if first.GetType() != waE2E.ButtonsMessage_Button_RESPONSE {
		t.Errorf("button[0].Type = %v, want RESPONSE", first.GetType())
	}
}

func TestBuildButtonsMessageCapsAtThree(t *testing.T) {
	msg := BuildButtonsMessage("body", "", []QuickReplyButton{
		{ID: "1", DisplayText: "1"},
		{ID: "2", DisplayText: "2"},
		{ID: "3", DisplayText: "3"},
		{ID: "4", DisplayText: "4"},
	})
	if got := len(msg.GetButtonsMessage().GetButtons()); got != MaxQuickReplyButtons {
		t.Errorf("got %d buttons, want %d", got, MaxQuickReplyButtons)
	}
	if msg.GetButtonsMessage().GetFooterText() != "" {
		t.Errorf("empty footer should not be set, got %q", msg.GetButtonsMessage().GetFooterText())
	}
}

func TestBuildTemplateMessage(t *testing.T) {
	msg := BuildTemplateMessage("Body text", "footer", []*waE2E.HydratedTemplateButton{
		NewQuickReplyTemplateButton("Reply", "reply-id"),
		NewURLTemplateButton("Visit", "https://example.com"),
		NewCallTemplateButton("Call", "+5511999999999"),
	})
	tm := msg.GetTemplateMessage()
	if tm == nil {
		t.Fatal("TemplateMessage is nil")
	}
	tmpl := tm.GetHydratedFourRowTemplate()
	if tmpl == nil {
		t.Fatal("HydratedFourRowTemplate is nil")
	}
	// The convenience HydratedTemplate field should point at the same template.
	if tm.GetHydratedTemplate() != tmpl {
		t.Error("HydratedTemplate and Format oneof should reference the same template")
	}
	if tmpl.GetHydratedContentText() != "Body text" {
		t.Errorf("content = %q, want %q", tmpl.GetHydratedContentText(), "Body text")
	}
	if tmpl.GetHydratedFooterText() != "footer" {
		t.Errorf("footer = %q, want %q", tmpl.GetHydratedFooterText(), "footer")
	}
	btns := tmpl.GetHydratedButtons()
	if len(btns) != 3 {
		t.Fatalf("got %d buttons, want 3", len(btns))
	}
	for i, btn := range btns {
		if btn.GetIndex() != uint32(i+1) {
			t.Errorf("button[%d].Index = %d, want %d", i, btn.GetIndex(), i+1)
		}
	}
	if btns[0].GetQuickReplyButton().GetID() != "reply-id" {
		t.Errorf("quick reply id = %q, want %q", btns[0].GetQuickReplyButton().GetID(), "reply-id")
	}
	if btns[1].GetUrlButton().GetURL() != "https://example.com" {
		t.Errorf("url = %q, want %q", btns[1].GetUrlButton().GetURL(), "https://example.com")
	}
	if btns[2].GetCallButton().GetPhoneNumber() != "+5511999999999" {
		t.Errorf("phone = %q, want %q", btns[2].GetCallButton().GetPhoneNumber(), "+5511999999999")
	}
}

func TestBuildTemplateMessagePreservesExplicitIndex(t *testing.T) {
	btn := NewQuickReplyTemplateButton("Reply", "id")
	btn.Index = proto.Uint32(7)
	msg := BuildTemplateMessage("body", "", []*waE2E.HydratedTemplateButton{btn})
	if got := msg.GetTemplateMessage().GetHydratedFourRowTemplate().GetHydratedButtons()[0].GetIndex(); got != 7 {
		t.Errorf("Index = %d, want 7 (explicit index should be preserved)", got)
	}
}

func TestBuildListMessage(t *testing.T) {
	msg := BuildListMessage("Title", "Body", "Open", "footer", []ListSection{
		{
			Title: "Section 1",
			Rows: []ListRow{
				{Title: "Row 1", Description: "desc 1", RowID: "row-1"},
				{Title: "Row 2", RowID: "row-2"},
			},
		},
	})
	lm := msg.GetListMessage()
	if lm == nil {
		t.Fatal("ListMessage is nil")
	}
	if lm.GetTitle() != "Title" || lm.GetDescription() != "Body" || lm.GetButtonText() != "Open" {
		t.Errorf("header fields wrong: title=%q desc=%q button=%q", lm.GetTitle(), lm.GetDescription(), lm.GetButtonText())
	}
	if lm.GetFooterText() != "footer" {
		t.Errorf("footer = %q, want %q", lm.GetFooterText(), "footer")
	}
	if lm.GetListType() != waE2E.ListMessage_SINGLE_SELECT {
		t.Errorf("ListType = %v, want SINGLE_SELECT", lm.GetListType())
	}
	if len(lm.GetSections()) != 1 {
		t.Fatalf("got %d sections, want 1", len(lm.GetSections()))
	}
	rows := lm.GetSections()[0].GetRows()
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].GetRowID() != "row-1" || rows[0].GetDescription() != "desc 1" {
		t.Errorf("row[0] wrong: id=%q desc=%q", rows[0].GetRowID(), rows[0].GetDescription())
	}
	// Row without a description should not set the field.
	if rows[1].Description != nil {
		t.Errorf("row[1].Description should be nil, got %q", rows[1].GetDescription())
	}
}

func TestNativeFlowButtonParamsJSON(t *testing.T) {
	tests := []struct {
		name   string
		button *waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton
		want   map[string]string
	}{
		{
			name:   nativeFlowQuickReply,
			button: NewQuickReplyNativeFlowButton("Yes", "yes-id"),
			want:   map[string]string{"display_text": "Yes", "id": "yes-id"},
		},
		{
			name:   nativeFlowCTAURL,
			button: NewURLNativeFlowButton("Visit", "https://example.com"),
			want:   map[string]string{"display_text": "Visit", "url": "https://example.com", "merchant_url": "https://example.com"},
		},
		{
			name:   nativeFlowCTACall,
			button: NewCallNativeFlowButton("Call", "+5511999999999"),
			want:   map[string]string{"display_text": "Call", "phone_number": "+5511999999999"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.button.GetName() != tc.name {
				t.Errorf("Name = %q, want %q", tc.button.GetName(), tc.name)
			}
			var got map[string]string
			if err := json.Unmarshal([]byte(tc.button.GetButtonParamsJSON()), &got); err != nil {
				t.Fatalf("ButtonParamsJSON is not valid JSON: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Errorf("params = %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("params[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestBuildInteractiveMessage(t *testing.T) {
	header := NewInteractiveHeaderText("Header", "Sub")
	msg := BuildInteractiveMessage("Body", "footer", header, []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
		NewQuickReplyNativeFlowButton("Yes", "yes"),
		NewQuickReplyNativeFlowButton("No", "no"),
	})
	im := msg.GetInteractiveMessage()
	if im == nil {
		t.Fatal("InteractiveMessage is nil")
	}
	if im.GetBody().GetText() != "Body" {
		t.Errorf("body = %q, want %q", im.GetBody().GetText(), "Body")
	}
	if im.GetFooter().GetText() != "footer" {
		t.Errorf("footer = %q, want %q", im.GetFooter().GetText(), "footer")
	}
	if im.GetHeader().GetTitle() != "Header" || im.GetHeader().GetSubtitle() != "Sub" {
		t.Errorf("header wrong: title=%q subtitle=%q", im.GetHeader().GetTitle(), im.GetHeader().GetSubtitle())
	}
	nf := im.GetNativeFlowMessage()
	if nf == nil {
		t.Fatal("NativeFlowMessage is nil")
	}
	if nf.GetMessageVersion() != interactiveMessageVersion {
		t.Errorf("MessageVersion = %d, want %d", nf.GetMessageVersion(), interactiveMessageVersion)
	}
	if len(nf.GetButtons()) != 2 {
		t.Errorf("got %d buttons, want 2", len(nf.GetButtons()))
	}
}

func TestBuildInteractiveMessageNoHeaderNoFooter(t *testing.T) {
	msg := BuildInteractiveMessage("Body", "", nil, nil)
	im := msg.GetInteractiveMessage()
	if im.GetHeader() != nil {
		t.Error("Header should be nil when not provided")
	}
	if im.GetFooter() != nil {
		t.Error("Footer should be nil when footer is empty")
	}
	if im.GetNativeFlowMessage() == nil {
		t.Error("NativeFlowMessage should still be present")
	}
}

// Carousel tests live in interactive_carousel_test.go.
