// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package host

import (
	"context"
	"errors"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/williamprado/whatsmeow/voip"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// newTestClient builds an unpaired, disconnected client backed by an in-memory
// store — enough to exercise the host's registry/lifecycle logic without a
// network.
func newTestClient(t *testing.T) *whatsmeow.Client {
	t.Helper()
	container, err := sqlstore.New(context.Background(), "sqlite",
		"file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)", waLog.Noop)
	if err != nil {
		t.Fatalf("sqlstore.New: %v", err)
	}
	t.Cleanup(func() { _ = container.Close() })
	return whatsmeow.NewClient(container.NewDevice(), waLog.Noop)
}

func TestAttachGetRemove(t *testing.T) {
	h := New(Config{}, nil, nil)

	s, err := h.Attach("111@s.whatsapp.net", newTestClient(t))
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if s.Manager == nil || s.Client == nil {
		t.Fatal("session missing manager or client")
	}
	if _, err := h.Attach("111@s.whatsapp.net", newTestClient(t)); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate Attach err = %v, want ErrDuplicate", err)
	}
	if _, err := h.Attach("222@s.whatsapp.net", newTestClient(t)); err != nil {
		t.Fatalf("second Attach: %v", err)
	}

	if got := h.Accounts(); len(got) != 2 || got[0] != "111@s.whatsapp.net" || got[1] != "222@s.whatsapp.net" {
		t.Fatalf("Accounts() = %v", got)
	}
	if _, ok := h.Get("111@s.whatsapp.net"); !ok {
		t.Fatal("Get(111) not found")
	}

	if err := h.Remove("111@s.whatsapp.net"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := h.Remove("111@s.whatsapp.net"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("second Remove err = %v, want ErrNoSession", err)
	}
	if h.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", h.Len())
	}
}

func TestActionsOnUnknownAccount(t *testing.T) {
	h := New(Config{}, nil, nil)
	ctx := context.Background()
	peer := types.NewJID("5577988272902", types.DefaultUserServer)

	if _, err := h.StartCall(ctx, "ghost", peer, false); !errors.Is(err, ErrNoSession) {
		t.Fatalf("StartCall err = %v, want ErrNoSession", err)
	}
	if err := h.AcceptCall(ctx, "ghost", "cid"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("AcceptCall err = %v, want ErrNoSession", err)
	}
	if err := h.EndCall(ctx, "ghost", "cid", ""); !errors.Is(err, ErrNoSession) {
		t.Fatalf("EndCall err = %v, want ErrNoSession", err)
	}
	if err := h.FeedCapturedPCM("ghost", "cid", nil); !errors.Is(err, ErrNoSession) {
		t.Fatalf("FeedCapturedPCM err = %v, want ErrNoSession", err)
	}
}

func TestDisabledManagerPropagates(t *testing.T) {
	// Manager config stays default (Enabled=false) — StartCall on a hosted
	// account must surface voip.ErrDisabled, not a host error.
	h := New(Config{}, nil, nil)
	if _, err := h.Attach("111@s.whatsapp.net", newTestClient(t)); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	_, err := h.StartCall(context.Background(), "111@s.whatsapp.net",
		types.NewJID("5577988272902", types.DefaultUserServer), false)
	if !errors.Is(err, voip.ErrDisabled) {
		t.Fatalf("StartCall err = %v, want voip.ErrDisabled", err)
	}
}

func TestActiveCallsAndShutdown(t *testing.T) {
	h := New(Config{MaxActiveCalls: 4}, nil, nil)
	if _, err := h.Attach("111@s.whatsapp.net", newTestClient(t)); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if _, err := h.Attach("222@s.whatsapp.net", newTestClient(t)); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if n := h.ActiveCalls(); n != 0 {
		t.Fatalf("ActiveCalls() = %d, want 0", n)
	}

	h.Shutdown()
	if h.Len() != 0 {
		t.Fatalf("Len() after Shutdown = %d, want 0", h.Len())
	}
	// Reusable after shutdown.
	if _, err := h.Attach("111@s.whatsapp.net", newTestClient(t)); err != nil {
		t.Fatalf("Attach after Shutdown: %v", err)
	}
}
