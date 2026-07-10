// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package voip

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.mau.fi/util/random"

	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Config configures the voip Manager.
type Config struct {
	// Enabled gates the entire VoIP subsystem. Default false. When false the
	// Manager registers no event handler and Reject/Terminate return ErrDisabled.
	// ⚠️ Only enable on a DISPOSABLE test account — never production.
	Enabled bool
}

// IncomingCall describes a received call offer.
type IncomingCall struct {
	CallID      string
	From        types.JID // the caller
	CallCreator types.JID
	Timestamp   time.Time
	// Offer is the raw <offer> node (carries the encrypted call key). Used by
	// DecryptCallKey; nil for synthesized calls.
	Offer *waBinary.Node
}

// ErrDisabled is returned by actions when the Manager is not enabled.
var ErrDisabled = errors.New("voip: disabled (set Config.Enabled=true; DISPOSABLE accounts only)")

// Manager detects incoming calls and can reject/terminate them. Phase 0:
// receive-only, no audio. Everything is gated by Config.Enabled.
type Manager struct {
	cli     *whatsmeow.Client
	sock    VoipSocket
	enabled bool
	log     waLog.Logger

	mu         sync.Mutex
	handlerID  uint32
	onIncoming func(IncomingCall)
}

// NewManager creates a voip Manager for the given client. log may be nil.
func NewManager(cli *whatsmeow.Client, cfg Config, log waLog.Logger) *Manager {
	if log == nil {
		log = waLog.Noop
	}
	return &Manager{
		cli:     cli,
		sock:    NewVoipSocket(cli),
		enabled: cfg.Enabled,
		log:     log,
	}
}

// Enabled reports whether the VoIP subsystem is on.
func (m *Manager) Enabled() bool { return m.enabled }

// Socket exposes the VoipSocket adapter (for Phase 1 wiring/tests).
func (m *Manager) Socket() VoipSocket { return m.sock }

// OnIncomingCall sets the callback fired for each incoming call offer.
func (m *Manager) OnIncomingCall(fn func(IncomingCall)) {
	m.mu.Lock()
	m.onIncoming = fn
	m.mu.Unlock()
}

// Start registers the call event handler. It is a no-op (logs a notice) when
// disabled, so it is always safe to call.
func (m *Manager) Start() {
	if !m.enabled {
		m.log.Infof("VoIP disabled (Config.Enabled=false) — not listening for calls")
		return
	}
	m.log.Warnf("VoIP ENABLED (Phase 0: receive-only, no audio) — DISPOSABLE accounts only, ban risk")
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handlerID == 0 {
		m.handlerID = m.cli.AddEventHandler(m.handleEvent)
	}
}

// Stop removes the event handler.
func (m *Manager) Stop() {
	m.mu.Lock()
	id := m.handlerID
	m.handlerID = 0
	m.mu.Unlock()
	if id != 0 {
		m.cli.RemoveEventHandler(id)
	}
}

func (m *Manager) handleEvent(evt any) {
	switch e := evt.(type) {
	case *events.CallOffer:
		call := IncomingCall{
			CallID:      e.CallID,
			From:        e.From,
			CallCreator: e.CallCreator,
			Timestamp:   e.Timestamp,
			Offer:       e.Data,
		}
		m.log.Infof("Incoming call: id=%s from=%s creator=%s ts=%s", call.CallID, call.From, call.CallCreator, call.Timestamp.Format(time.RFC3339))
		m.mu.Lock()
		cb := m.onIncoming
		m.mu.Unlock()
		if cb != nil {
			cb(call)
		}
	case *events.CallTerminate:
		m.log.Infof("Call terminated: id=%s reason=%s", e.CallID, e.Reason)
	case *events.CallReject:
		m.log.Infof("Call rejected by peer: id=%s", e.CallID)
	}
}

// Reject sends a clean <call><reject/> for the given call. No audio is set up.
func (m *Manager) Reject(ctx context.Context, call IncomingCall) error {
	return m.sendCallAction(ctx, "reject", call)
}

// Terminate sends a clean <call><terminate/> for the given call.
func (m *Manager) Terminate(ctx context.Context, call IncomingCall) error {
	return m.sendCallAction(ctx, "terminate", call)
}

func (m *Manager) sendCallAction(ctx context.Context, action string, call IncomingCall) error {
	if !m.enabled {
		return ErrDisabled
	}
	m.log.Infof("Sending <call><%s> for id=%s to=%s", action, call.CallID, call.From)
	return m.sock.SendNode(ctx, buildCallActionNode(action, call))
}

// buildCallActionNode builds a plaintext <call to=…><action call-id call-creator/></call>
// node, matching the reference (WaCalls) reject/terminate stanzas.
func buildCallActionNode(action string, call IncomingCall) waBinary.Node {
	creator := call.CallCreator
	if creator.IsEmpty() {
		creator = call.From
	}
	return waBinary.Node{
		Tag:   "call",
		Attrs: waBinary.Attrs{"to": call.From, "id": generateStanzaID()},
		Content: []waBinary.Node{{
			Tag:   action,
			Attrs: waBinary.Attrs{"call-id": call.CallID, "call-creator": creator},
		}},
	}
}

// generateStanzaID returns 16 random bytes as uppercase hex (matches the
// reference GenerateCallStanzaID).
func generateStanzaID() string {
	return strings.ToUpper(hex.EncodeToString(random.Bytes(16)))
}

// DecryptCallKey Signal-decrypts the 32-byte call key from an incoming call's
// offer (the <enc> node encrypted for our device). This verifies the call-key
// crypto on real data without setting up any media. Returns ErrDisabled when off.
func (m *Manager) DecryptCallKey(ctx context.Context, call IncomingCall) ([]byte, error) {
	if !m.enabled {
		return nil, ErrDisabled
	}
	enc := findEncNode(call.Offer)
	if enc == nil {
		return nil, fmt.Errorf("voip: no <enc> node in offer for call %s", call.CallID)
	}
	return m.sock.DecryptCallKey(ctx, call.From, enc)
}

// StartCall initiates a 1:1 audio call to peer: it builds and sends the
// <call><offer> (USync + per-device call-key encryption) and parses the relay
// ack. NO media is set up (the audio transport is a later phase). Returns the
// call id and the parsed relay endpoints. ⚠️ disposable accounts only.
func (m *Manager) StartCall(ctx context.Context, peer types.JID) (string, *ParsedRelayAck, error) {
	if !m.enabled {
		return "", nil, ErrDisabled
	}
	callID := GenerateCallID()
	callKey := GenerateCallKey()
	offer, err := BuildOfferStanza(ctx, m.sock, callID, callKey, peer, false)
	if err != nil {
		return callID, nil, fmt.Errorf("build offer: %w", err)
	}
	m.log.Infof("Starting call id=%s to=%s (offer handshake, no audio)", callID, peer)
	ack, err := m.sock.Query(ctx, offer)
	if err != nil {
		return callID, nil, fmt.Errorf("send offer: %w", err)
	}
	relays := ParseRelayFromAck(ack)
	m.log.Infof("Call %s ack: %d relay endpoint(s), %d participant(s), uuid=%s", callID, len(relays.Relays), len(relays.ParticipantJids), relays.UUID)
	return callID, &relays, nil
}

// findEncNode returns the first <enc> node found (recursively) under n.
func findEncNode(n *waBinary.Node) *waBinary.Node {
	if n == nil {
		return nil
	}
	for _, c := range nodeChildren(n) {
		c := c
		if c.Tag == "enc" {
			return &c
		}
		if found := findEncNode(&c); found != nil {
			return found
		}
	}
	return nil
}
