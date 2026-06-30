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
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// fakeSocket records SendNode calls; crypto methods inherit the stubbed errors.
type fakeSocket struct {
	socket
	sent []waBinary.Node
}

func (f *fakeSocket) SendNode(ctx context.Context, node waBinary.Node) error {
	f.sent = append(f.sent, node)
	return nil
}

func testManager(sock VoipSocket, enabled bool) *Manager {
	return &Manager{sock: sock, enabled: enabled, log: waLog.Noop}
}

func sampleCall() IncomingCall {
	return IncomingCall{
		CallID:      "CALLID123",
		From:        types.NewJID("5577999999999", types.DefaultUserServer),
		CallCreator: types.NewJID("5577999999999", types.DefaultUserServer),
		Timestamp:   time.Unix(1700000000, 0),
	}
}

func TestBuildCallActionNode(t *testing.T) {
	call := sampleCall()
	for _, action := range []string{"reject", "terminate"} {
		node := buildCallActionNode(action, call)
		if node.Tag != "call" {
			t.Fatalf("outer tag = %q, want call", node.Tag)
		}
		if to, _ := node.Attrs["to"].(types.JID); to != call.From {
			t.Errorf("to = %v, want %v", node.Attrs["to"], call.From)
		}
		if id, _ := node.Attrs["id"].(string); len(id) != 32 {
			t.Errorf("stanza id = %q, want 32 hex chars", id)
		}
		kids := node.GetChildren()
		if len(kids) != 1 || kids[0].Tag != action {
			t.Fatalf("inner = %+v, want single <%s>", kids, action)
		}
		if kids[0].Attrs["call-id"] != "CALLID123" {
			t.Errorf("call-id = %v", kids[0].Attrs["call-id"])
		}
		if cc, _ := kids[0].Attrs["call-creator"].(types.JID); cc != call.CallCreator {
			t.Errorf("call-creator = %v", kids[0].Attrs["call-creator"])
		}
	}
}

func TestCallActionCreatorFallsBackToFrom(t *testing.T) {
	call := sampleCall()
	call.CallCreator = types.EmptyJID
	node := buildCallActionNode("reject", call)
	if cc, _ := node.GetChildren()[0].Attrs["call-creator"].(types.JID); cc != call.From {
		t.Errorf("call-creator = %v, want From %v (fallback)", node.GetChildren()[0].Attrs["call-creator"], call.From)
	}
}

func TestRejectDisabled(t *testing.T) {
	m := testManager(&fakeSocket{}, false)
	if err := m.Reject(context.Background(), sampleCall()); !errors.Is(err, ErrDisabled) {
		t.Errorf("Reject disabled = %v, want ErrDisabled", err)
	}
	if err := m.Terminate(context.Background(), sampleCall()); !errors.Is(err, ErrDisabled) {
		t.Errorf("Terminate disabled = %v, want ErrDisabled", err)
	}
}

func TestRejectEnabledSendsNode(t *testing.T) {
	fs := &fakeSocket{}
	m := testManager(fs, true)
	if err := m.Reject(context.Background(), sampleCall()); err != nil {
		t.Fatalf("Reject = %v", err)
	}
	if len(fs.sent) != 1 || fs.sent[0].GetChildren()[0].Tag != "reject" {
		t.Fatalf("expected one <call><reject> sent, got %+v", fs.sent)
	}
}

func TestHandleEventCallOffer(t *testing.T) {
	m := testManager(&fakeSocket{}, true)
	var got IncomingCall
	var called bool
	m.OnIncomingCall(func(c IncomingCall) { got = c; called = true })

	m.handleEvent(&events.CallOffer{
		BasicCallMeta: types.BasicCallMeta{
			CallID:      "X1",
			From:        types.NewJID("5577988888888", types.DefaultUserServer),
			CallCreator: types.NewJID("5577988888888", types.DefaultUserServer),
			Timestamp:   time.Unix(1700000001, 0),
		},
	})
	if !called {
		t.Fatal("OnIncomingCall callback not invoked for CallOffer")
	}
	if got.CallID != "X1" {
		t.Errorf("call id = %q, want X1", got.CallID)
	}

	// Terminate/Reject events must not invoke the incoming callback.
	called = false
	m.handleEvent(&events.CallTerminate{BasicCallMeta: types.BasicCallMeta{CallID: "X1"}, Reason: "timeout"})
	if called {
		t.Error("CallTerminate should not invoke the incoming callback")
	}
}

func TestGenerateStanzaID(t *testing.T) {
	a, b := generateStanzaID(), generateStanzaID()
	if len(a) != 32 {
		t.Errorf("len = %d, want 32", len(a))
	}
	if a == b {
		t.Error("expected distinct random ids")
	}
	for _, r := range a {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'F')) {
			t.Fatalf("non-uppercase-hex char %q in %q", r, a)
		}
	}
}

// --- VoipSocket adapter tests over a fake clientAPI ---

type fakeAPI struct {
	pn, lid      types.JID
	sent         []waBinary.Node
	resp         chan *waBinary.Node
	encPlaintext []byte
}

func (f *fakeAPI) OwnPN() types.JID  { return f.pn }
func (f *fakeAPI) OwnLID() types.JID { return f.lid }
func (f *fakeAPI) SendNode(ctx context.Context, n waBinary.Node) error {
	f.sent = append(f.sent, n)
	return nil
}
func (f *fakeAPI) WaitResponse(reqID string) chan *waBinary.Node       { return f.resp }
func (f *fakeAPI) CancelResponse(reqID string, ch chan *waBinary.Node) {}
func (f *fakeAPI) MakeDeviceIdentityNode() waBinary.Node {
	return waBinary.Node{Tag: "device-identity"}
}
func (f *fakeAPI) GetUSyncDevices(ctx context.Context, jids []types.JID) ([]types.JID, error) {
	return jids, nil
}
func (f *fakeAPI) ResolveLIDForPN(ctx context.Context, pn types.JID) types.JID { return f.lid }
func (f *fakeAPI) GetPrivacyToken(ctx context.Context, jid types.JID) ([]byte, error) {
	return []byte("tok"), nil
}
func (f *fakeAPI) GenerateMessageID() string { return "MSGID" }
func (f *fakeAPI) EncryptMessageForDevices(ctx context.Context, devices []types.JID, id string, plaintext, dsm []byte, encAttrs waBinary.Attrs) ([]waBinary.Node, bool, error) {
	// Echo a participant <to><enc> node per device; record the plaintext.
	f.encPlaintext = plaintext
	out := make([]waBinary.Node, len(devices))
	for i, d := range devices {
		out[i] = waBinary.Node{Tag: "to", Attrs: waBinary.Attrs{"jid": d}, Content: []waBinary.Node{{Tag: "enc", Content: plaintext}}}
	}
	return out, true, nil
}
func (f *fakeAPI) DecryptDM(ctx context.Context, child *waBinary.Node, from types.JID, isPreKey bool) ([]byte, error) {
	// Return whatever bytes the enc node carries (set up by the test).
	if b, ok := child.Content.([]byte); ok {
		return b, nil
	}
	return nil, nil
}

func TestSocketPlumbingAndStubs(t *testing.T) {
	resp := make(chan *waBinary.Node, 1)
	api := &fakeAPI{pn: types.NewJID("1", types.DefaultUserServer), lid: types.NewJID("2", types.HiddenUserServer), resp: resp}
	s := &socket{api: api}

	if s.OwnPN() != api.pn || s.OwnLID() != api.lid {
		t.Error("OwnPN/OwnLID mismatch")
	}
	if n, ok := s.AccountDeviceIdentityNode(); !ok || n.Tag != "device-identity" {
		t.Error("AccountDeviceIdentityNode wrong")
	}
	if err := s.AssertSessions(context.Background(), nil, false); err != nil {
		t.Errorf("AssertSessions should be no-op, got %v", err)
	}

	// Query: send + receive correlated response.
	resp <- &waBinary.Node{Tag: "result"}
	got, err := s.Query(context.Background(), waBinary.Node{Tag: "call", Attrs: waBinary.Attrs{"id": "abc"}})
	if err != nil || got == nil || got.Tag != "result" {
		t.Fatalf("Query = %v, %v", got, err)
	}

	// Query without an id errors.
	if _, err := s.Query(context.Background(), waBinary.Node{Tag: "call"}); err == nil {
		t.Error("Query without id should error")
	}

	// Call-key crypto roundtrip through the adapter + fake encrypt/decrypt.
	callKey := GenerateCallKey()
	dev := types.NewJID("3", types.DefaultUserServer)
	nodes, includeID, err := s.CreateParticipantNodes(context.Background(), []types.JID{dev}, callKey, waBinary.Attrs{"count": "0"})
	if err != nil || !includeID || len(nodes) != 1 {
		t.Fatalf("CreateParticipantNodes = %v, %v, %v", nodes, includeID, err)
	}
	// The encrypted plaintext must be the encoded call-key message.
	wantPlaintext, _ := encodeCallKeyMessage(callKey)
	if string(api.encPlaintext) != string(wantPlaintext) {
		t.Error("CreateParticipantNodes did not encode the call key into the plaintext")
	}
	// DecryptCallKey of an <enc> carrying that plaintext recovers the key.
	enc := &waBinary.Node{Tag: "enc", Attrs: waBinary.Attrs{"type": "pkmsg"}, Content: wantPlaintext}
	gotKey, err := s.DecryptCallKey(context.Background(), dev, enc)
	if err != nil {
		t.Fatalf("DecryptCallKey error: %v", err)
	}
	if string(gotKey) != string(callKey) {
		t.Error("DecryptCallKey did not recover the original call key")
	}
}

func TestCallKeyCodecRoundtrip(t *testing.T) {
	key := GenerateCallKey()
	if len(key) != CallKeyLen {
		t.Fatalf("GenerateCallKey len = %d, want %d", len(key), CallKeyLen)
	}
	pt, err := encodeCallKeyMessage(key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeCallKeyPlaintext(pt)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(key) {
		t.Error("call key did not survive encode/decode")
	}
	// A wrong-length key is rejected.
	bad, _ := encodeCallKeyMessage([]byte("short"))
	if _, err := decodeCallKeyPlaintext(bad); err == nil {
		t.Error("expected error for non-32-byte call key")
	}
}
