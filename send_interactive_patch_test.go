// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	"testing"

	"google.golang.org/protobuf/proto"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// These tests cover the custom fork patch in send_interactive_patch.go: the
// <biz> routing node type/attrs for the interactive message types upstream does
// not handle, plus a regression guard that ButtonsMessage/ListMessage are
// unchanged.

func TestButtonTypeForInteractiveTypes(t *testing.T) {
	tests := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{"interactive native flow", BuildInteractiveMessage("b", "", nil, nil), "interactive"},
		{"carousel", BuildCarouselMessage("b", []CarouselCard{{Body: "c"}}), "interactive"},
		{"template", BuildTemplateMessage("b", "", []*waE2E.HydratedTemplateButton{
			NewQuickReplyTemplateButton("x", "y"),
		}), "interactive"},
		// Regression: upstream-handled types must be unchanged.
		{"buttons", BuildButtonsMessage("b", "", []QuickReplyButton{{ID: "a", DisplayText: "A"}}), "buttons"},
		{"list", BuildListMessage("t", "d", "open", "", []ListSection{{Title: "s"}}), "list"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := getButtonTypeFromMessage(tc.msg); got != tc.want {
				t.Errorf("getButtonTypeFromMessage = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestButtonAttributesForInteractive(t *testing.T) {
	attrs := getButtonAttributes(BuildInteractiveMessage("b", "", nil, nil))
	if attrs["type"] != "native_flow" {
		t.Errorf("type = %v, want native_flow", attrs["type"])
	}
	if attrs["v"] != interactiveNodeVersion {
		t.Errorf("v = %v, want %s", attrs["v"], interactiveNodeVersion)
	}

	// Carousel rides the same native_flow routing node.
	carouselAttrs := getButtonAttributes(BuildCarouselMessage("b", []CarouselCard{{Body: "c"}}))
	if carouselAttrs["type"] != "native_flow" {
		t.Errorf("carousel type = %v, want native_flow", carouselAttrs["type"])
	}

	// Regression: list keeps its own attrs (v=2, single_select).
	listAttrs := getButtonAttributes(BuildListMessage("t", "d", "open", "", []ListSection{{Title: "s"}}))
	if listAttrs["v"] != "2" || listAttrs["type"] != "single_select" {
		t.Errorf("list attrs = %v, want v=2 type=single_select", listAttrs)
	}
}

func TestCustomInteractiveBizNodeNested(t *testing.T) {
	msg := BuildInteractiveMessage("body", "", nil, []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
		NewQuickReplyNativeFlowButton("Yes", "nf-yes"),
		NewURLNativeFlowButton("Site", "https://example.com"),
	})
	node, ok := customInteractiveBizNode(msg)
	if !ok || node == nil {
		t.Fatal("customInteractiveBizNode returned ok=false")
	}
	if node.Tag != "biz" {
		t.Fatalf("outer tag = %q, want biz", node.Tag)
	}
	interactive := node.GetChildren()
	if len(interactive) != 1 || interactive[0].Tag != "interactive" {
		t.Fatalf("expected single <interactive> child, got %+v", interactive)
	}
	if interactive[0].Attrs["type"] != "native_flow" || interactive[0].Attrs["v"] != interactiveNodeVersion {
		t.Errorf("interactive attrs = %v", interactive[0].Attrs)
	}
	nf := interactive[0].GetChildren()
	if len(nf) != 1 || nf[0].Tag != "native_flow" {
		t.Fatalf("expected single <native_flow> grandchild, got %+v", nf)
	}
	// quick_reply + cta_url -> "mixed" (default), and native_flow v must be "9".
	if nf[0].Attrs["name"] != "mixed" {
		t.Errorf("native_flow name = %v, want mixed", nf[0].Attrs["name"])
	}
	if nf[0].Attrs["v"] != nativeFlowVersion || nativeFlowVersion != "9" {
		t.Errorf("native_flow v = %v, want 9", nf[0].Attrs["v"])
	}
}

func TestNativeFlowNameMap(t *testing.T) {
	mk := func(name string) *waE2E.Message {
		return BuildInteractiveMessage("b", "", nil, []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
			{Name: proto.String(name)},
		})
	}
	tests := map[string]string{
		"review_and_pay": "payment_info",
		"payment_info":   "payment_info",
		"mpm":            "mpm",
		"review_order":   "order_details",
		"quick_reply":    "mixed",
		"cta_url":        "mixed",
		"anything_else":  "mixed",
	}
	for in, want := range tests {
		if got := nativeFlowName(mk(in)); got != want {
			t.Errorf("nativeFlowName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCustomInteractiveBizNodeSkipsButtons(t *testing.T) {
	// Buttons/list must not be handled by the custom biz node (upstream handles them).
	if _, ok := customInteractiveBizNode(BuildButtonsMessage("b", "", nil)); ok {
		t.Error("customInteractiveBizNode should not handle ButtonsMessage")
	}
}

func bizBotTags(node *waBinary.Node) []string {
	var tags []string
	for _, c := range node.GetChildren() {
		tags = append(tags, c.Tag)
	}
	return tags
}

func TestRelocateBizAndAddBotForList(t *testing.T) {
	// Simulate a stanza after getMessageContent (biz) + tctoken were appended.
	msg := BuildListMessage("t", "d", "open", "", []ListSection{{Title: "s", Rows: []ListRow{{Title: "r", RowID: "r1"}}}})
	node := &waBinary.Node{Tag: "message", Content: []waBinary.Node{
		{Tag: "enc"},
		{Tag: "biz", Content: []waBinary.Node{{Tag: "list"}}},
		{Tag: "tctoken"},
	}}
	to := types.NewJID("5577988272902", types.DefaultUserServer)
	(*Client)(nil).relocateInteractiveBizAndAddBot(node, to, msg)

	got := bizBotTags(node)
	// biz must move after tctoken, and bot must be last.
	want := []string{"enc", "tctoken", "biz", "bot"}
	if len(got) != len(want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tags = %v, want %v", got, want)
		}
	}
	// bot must carry biz_bot="1".
	bot := node.GetChildren()[3]
	if bot.Attrs["biz_bot"] != "1" {
		t.Errorf("bot biz_bot = %v, want 1", bot.Attrs["biz_bot"])
	}
}

func TestRelocateNoBotForCarousel(t *testing.T) {
	msg := BuildCarouselMessage("b", []CarouselCard{{Body: "c"}})
	node := &waBinary.Node{Tag: "message", Content: []waBinary.Node{
		{Tag: "biz", Content: []waBinary.Node{{Tag: "interactive"}}},
		{Tag: "tctoken"},
	}}
	to := types.NewJID("5577988272902", types.DefaultUserServer)
	(*Client)(nil).relocateInteractiveBizAndAddBot(node, to, msg)

	got := bizBotTags(node)
	// biz relocated after tctoken, but NO bot for carousel.
	want := []string{"tctoken", "biz"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("tags = %v, want %v (no bot for carousel)", got, want)
	}
}

func TestRelocateSkipsGroups(t *testing.T) {
	msg := BuildButtonsMessage("b", "", []QuickReplyButton{{ID: "a", DisplayText: "A"}})
	orig := []waBinary.Node{{Tag: "biz"}, {Tag: "tctoken"}}
	node := &waBinary.Node{Tag: "message", Content: append([]waBinary.Node{}, orig...)}
	to := types.NewJID("123456789", types.GroupServer)
	(*Client)(nil).relocateInteractiveBizAndAddBot(node, to, msg)
	if got := bizBotTags(node); len(got) != 2 || got[0] != "biz" || got[1] != "tctoken" {
		t.Errorf("group stanza must be untouched, got %v", got)
	}
}

func TestRelocateSkipsNonInteractive(t *testing.T) {
	msg := &waE2E.Message{Conversation: proto.String("hi")}
	node := &waBinary.Node{Tag: "message", Content: []waBinary.Node{{Tag: "enc"}, {Tag: "tctoken"}}}
	to := types.NewJID("5577988272902", types.DefaultUserServer)
	(*Client)(nil).relocateInteractiveBizAndAddBot(node, to, msg)
	if got := bizBotTags(node); len(got) != 2 {
		t.Errorf("plain text stanza must be untouched, got %v", got)
	}
}

func TestButtonTypeUnwrapsInteractiveInViewOnce(t *testing.T) {
	// The hook runs at the top of getButtonTypeFromMessage; a wrapped interactive
	// message must still resolve via the existing recursion.
	inner := BuildInteractiveMessage("b", "", nil, nil)
	wrapped := &waE2E.Message{
		ViewOnceMessage: &waE2E.FutureProofMessage{Message: inner},
	}
	if got := getButtonTypeFromMessage(wrapped); got != "interactive" {
		t.Errorf("wrapped interactive type = %q, want interactive", got)
	}
}
