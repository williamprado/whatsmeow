// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
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
