// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package voip is the companion module that adds WhatsApp voice-call (VoIP)
// support to this whatsmeow fork. It is a SEPARATE Go module so the heavy media
// dependency tree (pion, MLow codec, SRTP/STUN) never touches the main
// go.mau.fi/whatsmeow go.mod.
//
// The call logic (signaling, MLow codec, SRTP, relay transport, state machine)
// was ported from the reference williamprado/WaCalls
// (JotaDev66/WaCalls, branch feat/native-mlow-pcm-transport). This top-level
// Manager wires that logic onto a *whatsmeow.Client from this fork.
//
// ⚠️ VERY HIGH account-ban risk. Everything is gated behind Config.Enabled
// (default false) and must be exercised only on disposable accounts — never the
// production account, never the production pin.
package voip

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/williamprado/whatsmeow/voip/call"
	"github.com/williamprado/whatsmeow/voip/core"
	"github.com/williamprado/whatsmeow/voip/signaling"
	"github.com/williamprado/whatsmeow/voip/wa"
	"github.com/williamprado/whatsmeow/voip/wanode"

	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// ErrDisabled is returned by every call action when Config.Enabled is false.
var ErrDisabled = errors.New("voip: disabled (set Config.Enabled = true)")

// ErrNoSuchCall is returned when an action references an unknown call id.
var ErrNoSuchCall = errors.New("voip: no active call with that id")

// ErrBusy is returned by StartCall/incoming offers when the concurrent-call
// limit (Config.MaxConcurrentCalls) is reached.
var ErrBusy = errors.New("voip: concurrent call limit reached")

// Config controls the VoIP manager.
type Config struct {
	// Enabled must be true for any call to be sent, accepted, or answered.
	// Default false — receiving offers still emits OnIncomingCall so the caller
	// can decide, but no media/crypto is set up unless enabled.
	Enabled bool
	// MaxConcurrentCalls caps simultaneous calls (0 = unlimited). Extra inbound
	// offers are auto-rejected.
	MaxConcurrentCalls int
}

// Manager owns per-call state machines and bridges whatsmeow call events into
// them. Create one per *whatsmeow.Client.
type Manager struct {
	client *whatsmeow.Client
	sock   *wa.Socket
	log    *slog.Logger
	cfg    Config

	mu    sync.Mutex
	calls map[string]*call.CallManager

	onIncoming    func(*call.CallInfo)
	onStateChange func(*call.CallInfo)
	onEnded       func(*call.CallInfo)
	onPeerAudio   func(callID string, pcm16 []float32)

	handlerID uint32
	started   bool
}

// New creates a VoIP manager for client. log may be nil (slog.Default is used).
func New(client *whatsmeow.Client, cfg Config, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		client: client,
		sock:   wa.NewSocket(client),
		log:    log,
		cfg:    cfg,
		calls:  make(map[string]*call.CallManager),
	}
}

// Start subscribes to the client's call events. Safe to call once.
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return
	}
	m.handlerID = m.client.AddEventHandler(m.handleEvent)
	m.started = true
	m.log.Info("voip manager started", "enabled", m.cfg.Enabled)
}

// Stop unsubscribes and tears down every active call.
func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	m.client.RemoveEventHandler(m.handlerID)
	m.started = false
	active := make([]*call.CallManager, 0, len(m.calls))
	for _, cm := range m.calls {
		active = append(active, cm)
	}
	m.calls = make(map[string]*call.CallManager)
	m.mu.Unlock()

	for _, cm := range active {
		_ = cm.EndCall(context.Background(), core.EndCallReasonUserEnded)
	}
}

// OnIncomingCall registers a callback fired when a new inbound call rings.
func (m *Manager) OnIncomingCall(fn func(*call.CallInfo)) { m.onIncoming = fn }

// OnCallStateChange registers a callback fired on every call state transition.
func (m *Manager) OnCallStateChange(fn func(*call.CallInfo)) { m.onStateChange = fn }

// OnCallEnded registers a callback fired once when a call ends.
func (m *Manager) OnCallEnded(fn func(*call.CallInfo)) { m.onEnded = fn }

// OnPeerAudio registers a callback delivering decoded 16 kHz mono float32 PCM
// from the remote party, tagged with the call id.
func (m *Manager) OnPeerAudio(fn func(callID string, pcm16 []float32)) { m.onPeerAudio = fn }

// StartCall places an outbound call to peer and returns the generated call id.
func (m *Manager) StartCall(ctx context.Context, peer types.JID, isVideo bool) (string, error) {
	if !m.cfg.Enabled {
		return "", ErrDisabled
	}
	if !m.hasCapacity() {
		return "", ErrBusy
	}
	callID := signaling.GenerateCallID()
	cm := m.newCallManager(callID)
	if err := cm.StartCall(ctx, callID, peer, isVideo); err != nil {
		m.removeCall(callID)
		return "", err
	}
	return callID, nil
}

// AcceptCall answers a ringing inbound call.
func (m *Manager) AcceptCall(ctx context.Context, callID string) error {
	if !m.cfg.Enabled {
		return ErrDisabled
	}
	cm, ok := m.getCall(callID)
	if !ok {
		return ErrNoSuchCall
	}
	return cm.AcceptCall(ctx, callID)
}

// RejectCall declines a ringing inbound call.
func (m *Manager) RejectCall(ctx context.Context, callID string, reason core.EndCallReason) error {
	if reason == "" {
		reason = core.EndCallReasonDeclined
	}
	cm, ok := m.getCall(callID)
	if !ok {
		return ErrNoSuchCall
	}
	return cm.RejectCall(ctx, callID, reason)
}

// EndCall hangs up an active or outbound call.
func (m *Manager) EndCall(ctx context.Context, callID string, reason core.EndCallReason) error {
	if reason == "" {
		reason = core.EndCallReasonUserEnded
	}
	cm, ok := m.getCall(callID)
	if !ok {
		return ErrNoSuchCall
	}
	return cm.EndCall(ctx, reason)
}

// FeedCapturedPCM pushes locally-captured 16 kHz mono float32 microphone PCM
// into the call's encoder → SRTP → relay path.
func (m *Manager) FeedCapturedPCM(callID string, pcm16 []float32) error {
	cm, ok := m.getCall(callID)
	if !ok {
		return ErrNoSuchCall
	}
	cm.FeedCapturedPCM(pcm16)
	return nil
}

// CurrentCall returns the CallInfo for a given id, if present.
func (m *Manager) CurrentCall(callID string) (*call.CallInfo, bool) {
	cm, ok := m.getCall(callID)
	if !ok {
		return nil, false
	}
	return cm.CurrentCall(), true
}

// --- internals ---

func (m *Manager) hasCapacity() bool {
	if m.cfg.MaxConcurrentCalls <= 0 {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls) < m.cfg.MaxConcurrentCalls
}

func (m *Manager) getCall(callID string) (*call.CallManager, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cm, ok := m.calls[callID]
	return cm, ok
}

func (m *Manager) removeCall(callID string) {
	m.mu.Lock()
	delete(m.calls, callID)
	m.mu.Unlock()
}

func (m *Manager) newCallManager(callID string) *call.CallManager {
	cm := call.NewCallManager(m.sock, m.log)
	cm.OnIncoming = func(c *call.CallInfo) {
		if m.onIncoming != nil {
			m.onIncoming(c)
		}
	}
	cm.OnStateChange = func(c *call.CallInfo) {
		if c.IsEnded() {
			m.removeCall(c.CallID)
		}
		if m.onStateChange != nil {
			m.onStateChange(c)
		}
	}
	cm.OnEnded = func(c *call.CallInfo) {
		m.removeCall(c.CallID)
		if m.onEnded != nil {
			m.onEnded(c)
		}
	}
	cm.OnPeerAudio = func(pcm16 []float32) {
		if m.onPeerAudio != nil {
			m.onPeerAudio(callID, pcm16)
		}
	}
	m.mu.Lock()
	m.calls[callID] = cm
	m.mu.Unlock()
	return cm
}

func (m *Manager) handleEvent(rawEvt any) {
	ctx := context.Background()
	switch evt := rawEvt.(type) {
	case *events.CallOffer:
		m.onIncomingOffer(ctx, evt.From, evt.Data)
	case *events.CallAccept:
		if cm, ok := m.callForNode(evt.From, evt.Data); ok {
			cm.HandleCallAccept(ctx, wrapCall(evt.From, evt.Data), evt.From)
		}
	case *events.CallTransport:
		if cm, ok := m.callForNode(evt.From, evt.Data); ok {
			cm.HandleCallTransport(ctx, wrapCall(evt.From, evt.Data), evt.From)
		}
	case *events.CallTerminate:
		if cm, ok := m.callForNode(evt.From, evt.Data); ok {
			cm.HandleCallTerminate(wrapCall(evt.From, evt.Data))
		}
	case *events.CallReject:
		if cm, ok := m.callForNode(evt.From, evt.Data); ok {
			cm.HandleCallTerminate(wrapCall(evt.From, evt.Data))
		}
	}
}

func (m *Manager) onIncomingOffer(ctx context.Context, from types.JID, data *waBinary.Node) {
	node := wrapCall(from, data)
	callID := callIDFromNode(node)
	if callID == "" {
		return
	}
	// If the manager is disabled, or we are at capacity, reject the offer.
	if !m.cfg.Enabled || !m.hasCapacity() {
		m.rejectOffer(ctx, node, from)
		return
	}
	cm := m.newCallManager(callID)
	cm.HandleCallOffer(ctx, node, from)
}

func (m *Manager) rejectOffer(ctx context.Context, node *waBinary.Node, from types.JID) {
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return
	}
	creator := wanode.AttrString(info.InnerNode.Attrs, "call-creator")
	if creator == "" {
		creator = from.String()
	}
	reject := signaling.BuildRejectStanza(from, info.CallID, wanode.MustJID(creator))
	_ = m.sock.SendNode(ctx, reject)
	m.log.Info("inbound call rejected (disabled or at capacity)", "call_id", info.CallID)
}

func (m *Manager) callForNode(from types.JID, data *waBinary.Node) (*call.CallManager, bool) {
	callID := callIDFromNode(wrapCall(from, data))
	if callID == "" {
		return nil, false
	}
	return m.getCall(callID)
}

// wrapCall rebuilds the <call from><inner/></call> envelope that the parsers in
// the signaling/ and call/ packages expect (whatsmeow delivers only the inner
// node via the event).
func wrapCall(from types.JID, inner *waBinary.Node) *waBinary.Node {
	content := []waBinary.Node{}
	if inner != nil {
		content = append(content, *inner)
	}
	return &waBinary.Node{
		Tag:     "call",
		Attrs:   waBinary.Attrs{"from": from},
		Content: content,
	}
}

func callIDFromNode(node *waBinary.Node) string {
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return ""
	}
	return info.CallID
}
