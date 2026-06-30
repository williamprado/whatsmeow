// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package voip

import (
	"context"
	"testing"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

func childByTag(n waBinary.Node, tag string) *waBinary.Node {
	for _, c := range nodeChildren(&n) {
		c := c
		if c.Tag == tag {
			return &c
		}
	}
	return nil
}

func TestBuildOfferStanza(t *testing.T) {
	api := &fakeAPI{
		pn:  types.NewJID("1", types.DefaultUserServer),
		lid: types.NewJID("2", types.HiddenUserServer),
	}
	s := &socket{api: api}
	peer := types.NewJID("5577999999999", types.DefaultUserServer)

	node, err := BuildOfferStanza(context.Background(), s, "CALLID", GenerateCallKey(), peer, false)
	if err != nil {
		t.Fatalf("BuildOfferStanza: %v", err)
	}
	if node.Tag != "call" {
		t.Fatalf("outer tag = %q, want call", node.Tag)
	}
	offer := childByTag(node, "offer")
	if offer == nil {
		t.Fatal("no <offer> child")
	}
	if offer.Attrs["call-id"] != "CALLID" {
		t.Errorf("call-id = %v", offer.Attrs["call-id"])
	}
	// creator should be the LID (preferred).
	if cc, _ := offer.Attrs["call-creator"].(types.JID); cc != api.lid {
		t.Errorf("call-creator = %v, want LID %v", offer.Attrs["call-creator"], api.lid)
	}
	for _, tag := range []string{"audio", "net", "capability", "destination", "encopt"} {
		if childByTag(*offer, tag) == nil {
			t.Errorf("offer missing <%s>", tag)
		}
	}
	// destination must carry the per-device participant node.
	dest := childByTag(*offer, "destination")
	if dest == nil || len(nodeChildren(dest)) == 0 {
		t.Error("destination has no participant nodes")
	}
}

func TestParseRelayFromAck(t *testing.T) {
	hbh := make([]byte, 30)
	for i := range hbh {
		hbh[i] = byte(i)
	}
	addr := []byte{1, 2, 3, 4, 0x0d, 0x90} // 1.2.3.4 : 0x0d90 = 3472
	ack := &waBinary.Node{Tag: "ack", Content: []waBinary.Node{{
		Tag:   "relay",
		Attrs: waBinary.Attrs{"uuid": "U-1", "self_pid": "1", "peer_pid": "2"},
		Content: []waBinary.Node{
			{Tag: "participant", Attrs: waBinary.Attrs{"jid": "5577999999999@s.whatsapp.net"}},
			{Tag: "key", Content: []byte("relaykey")},
			{Tag: "hbh_key", Content: hbh},
			{Tag: "token", Attrs: waBinary.Attrs{"id": "0"}, Content: []byte("tok0")},
			{Tag: "te2", Attrs: waBinary.Attrs{"token_id": "0", "relay_id": "5", "relay_name": "r1"}, Content: addr},
		},
	}}}

	res := ParseRelayFromAck(ack)
	if res.UUID != "U-1" {
		t.Errorf("uuid = %q", res.UUID)
	}
	if res.SelfPid == nil || *res.SelfPid != 1 || res.PeerPid == nil || *res.PeerPid != 2 {
		t.Errorf("pids = %v/%v", res.SelfPid, res.PeerPid)
	}
	if len(res.HbhKey) != 30 {
		t.Errorf("hbh key len = %d, want 30", len(res.HbhKey))
	}
	if len(res.Relays) != 1 {
		t.Fatalf("relays = %d, want 1", len(res.Relays))
	}
	r := res.Relays[0]
	if r.IP != "1.2.3.4" || r.Port != 3472 {
		t.Errorf("endpoint = %s:%d, want 1.2.3.4:3472", r.IP, r.Port)
	}
	if r.RelayID != 5 || r.RelayName != "r1" {
		t.Errorf("relay meta = %d/%s", r.RelayID, r.RelayName)
	}
	if len(res.ParticipantJids) != 1 {
		t.Errorf("participants = %v", res.ParticipantJids)
	}
}
