// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package voip

import (
	"context"
	"errors"
	"testing"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

// A disabled manager must gate every outbound/answer action without ever
// touching the (nil) client.
func TestDisabledGate(t *testing.T) {
	m := New(nil, Config{Enabled: false}, nil)
	peer := types.NewJID("5577999999999", types.DefaultUserServer)

	if _, err := m.StartCall(context.Background(), peer, false); !errors.Is(err, ErrDisabled) {
		t.Errorf("StartCall disabled = %v, want ErrDisabled", err)
	}
	if err := m.AcceptCall(context.Background(), "ABC"); !errors.Is(err, ErrDisabled) {
		t.Errorf("AcceptCall disabled = %v, want ErrDisabled", err)
	}
}

// Actions on an unknown call id return ErrNoSuchCall.
func TestUnknownCall(t *testing.T) {
	m := New(nil, Config{Enabled: true}, nil)
	if err := m.RejectCall(context.Background(), "NOPE", ""); !errors.Is(err, ErrNoSuchCall) {
		t.Errorf("RejectCall unknown = %v, want ErrNoSuchCall", err)
	}
	if err := m.EndCall(context.Background(), "NOPE", ""); !errors.Is(err, ErrNoSuchCall) {
		t.Errorf("EndCall unknown = %v, want ErrNoSuchCall", err)
	}
	if err := m.FeedCapturedPCM("NOPE", nil); !errors.Is(err, ErrNoSuchCall) {
		t.Errorf("FeedCapturedPCM unknown = %v, want ErrNoSuchCall", err)
	}
	if _, ok := m.CurrentCall("NOPE"); ok {
		t.Error("CurrentCall unknown should be !ok")
	}
}

// wrapCall + callIDFromNode round-trip the inner offer/accept/transport node.
func TestWrapAndExtractCallID(t *testing.T) {
	from := types.NewJID("5577988888888", types.DefaultUserServer)
	inner := &waBinary.Node{
		Tag:   "offer",
		Attrs: waBinary.Attrs{"call-id": "CALLID-XYZ", "call-creator": from},
	}
	node := wrapCall(from, inner)
	if node.Tag != "call" {
		t.Fatalf("outer tag = %q, want call", node.Tag)
	}
	if got := callIDFromNode(node); got != "CALLID-XYZ" {
		t.Errorf("callIDFromNode = %q, want CALLID-XYZ", got)
	}
	// A node with no inner child yields an empty id (not a panic).
	if got := callIDFromNode(&waBinary.Node{Tag: "call"}); got != "" {
		t.Errorf("callIDFromNode(empty) = %q, want empty", got)
	}
}
