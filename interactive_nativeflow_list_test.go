// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	"encoding/json"
	"strings"
	"testing"
)

func sampleSections(n, rowsPer int) []ListSection {
	secs := make([]ListSection, n)
	for i := range secs {
		rows := make([]ListRow, rowsPer)
		for j := range rows {
			rows[j] = ListRow{Title: "row", RowID: "r", Description: "d"}
		}
		secs[i] = ListSection{Title: "sec", Rows: rows}
	}
	return secs
}

func TestBuildNativeFlowListMessage(t *testing.T) {
	msg, err := BuildNativeFlowListMessage("Open menu", "Pick one", "footer",
		NewInteractiveHeaderText("Menu", ""),
		[]ListSection{
			{Title: "Drinks", Rows: []ListRow{
				{Title: "Coffee", Description: "hot", RowID: "coffee"},
				{Title: "Tea", RowID: "tea"},
			}},
			{Title: "Food", Rows: []ListRow{{Title: "Cake", RowID: "cake"}}},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must be wrapped in ViewOnceMessage.
	if msg.GetViewOnceMessage() == nil || msg.GetViewOnceMessage().GetMessage() == nil {
		t.Fatal("expected ViewOnceMessage wrapper")
	}
	im := msg.GetViewOnceMessage().GetMessage().GetInteractiveMessage()
	if im == nil {
		t.Fatal("expected InteractiveMessage inside ViewOnce")
	}
	if im.GetBody().GetText() != "Pick one" {
		t.Errorf("body = %q", im.GetBody().GetText())
	}
	if im.GetFooter().GetText() != "footer" {
		t.Errorf("footer = %q", im.GetFooter().GetText())
	}
	if im.GetHeader().GetTitle() != "Menu" {
		t.Errorf("header title = %q", im.GetHeader().GetTitle())
	}

	nf := im.GetNativeFlowMessage()
	if nf == nil {
		t.Fatal("expected NativeFlowMessage")
	}
	if nf.GetMessageVersion() != 2 {
		t.Errorf("messageVersion = %d, want 2", nf.GetMessageVersion())
	}
	if nf.GetMessageParamsJSON() != "{}" {
		t.Errorf("messageParamsJSON = %q, want {}", nf.GetMessageParamsJSON())
	}
	if len(nf.GetButtons()) != 1 {
		t.Fatalf("got %d buttons, want 1", len(nf.GetButtons()))
	}
	btn := nf.GetButtons()[0]
	if btn.GetName() != nativeFlowSingleSelect {
		t.Errorf("button name = %q, want %q", btn.GetName(), nativeFlowSingleSelect)
	}

	// buttonParamsJSON shape.
	var params nativeFlowListParams
	if err := json.Unmarshal([]byte(btn.GetButtonParamsJSON()), &params); err != nil {
		t.Fatalf("buttonParamsJSON invalid: %v", err)
	}
	if params.Title != "Open menu" {
		t.Errorf("params title = %q, want Open menu", params.Title)
	}
	if len(params.Sections) != 2 {
		t.Fatalf("params sections = %d, want 2", len(params.Sections))
	}
	if params.Sections[0].Title != "Drinks" || len(params.Sections[0].Rows) != 2 {
		t.Errorf("section[0] = %+v", params.Sections[0])
	}
	if params.Sections[0].Rows[0].ID != "coffee" || params.Sections[0].Rows[0].Title != "Coffee" || params.Sections[0].Rows[0].Description != "hot" {
		t.Errorf("row[0] = %+v", params.Sections[0].Rows[0])
	}

	// Must route through the interactive/native_flow biz node + bot patch.
	if got := getButtonTypeFromMessage(msg); got != "interactive" {
		t.Errorf("getButtonTypeFromMessage = %q, want interactive (so bot node applies)", got)
	}
}

func TestBuildNativeFlowListLimits(t *testing.T) {
	if _, err := BuildNativeFlowListMessage("b", "body", "", nil, nil); err == nil {
		t.Error("expected error for zero sections")
	}
	if _, err := BuildNativeFlowListMessage("b", "body", "", nil, sampleSections(MaxListSections+1, 1)); err == nil {
		t.Error("expected error for too many sections")
	}
	if _, err := BuildNativeFlowListMessage("b", "body", "", nil, sampleSections(1, MaxListRowsPerSec+1)); err == nil {
		t.Error("expected error for too many rows in a section")
	}
	// Empty section rows.
	if _, err := BuildNativeFlowListMessage("b", "body", "", nil, []ListSection{{Title: "s"}}); err == nil || !strings.Contains(err.Error(), "no rows") {
		t.Errorf("expected 'no rows' error, got %v", err)
	}
	// Valid maximal list should succeed.
	if _, err := BuildNativeFlowListMessage("b", "body", "", nil, sampleSections(MaxListSections, MaxListRowsPerSec)); err != nil {
		t.Errorf("maximal valid list should not error: %v", err)
	}
}

func TestBuildNativeFlowQuickReplyMessage(t *testing.T) {
	msg := BuildNativeFlowQuickReplyMessage("Choose", "", nil, []QuickReplyButton{
		{ID: "yes", DisplayText: "Yes"},
		{ID: "no", DisplayText: "No"},
	})
	nf := msg.GetInteractiveMessage().GetNativeFlowMessage()
	if nf == nil || len(nf.GetButtons()) != 2 {
		t.Fatalf("expected 2 native-flow buttons")
	}
	if nf.GetButtons()[0].GetName() != nativeFlowQuickReply {
		t.Errorf("button name = %q, want quick_reply", nf.GetButtons()[0].GetName())
	}
	// Routes through the interactive biz node.
	if got := getButtonTypeFromMessage(msg); got != "interactive" {
		t.Errorf("getButtonTypeFromMessage = %q, want interactive", got)
	}
}
