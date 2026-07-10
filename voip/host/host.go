// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package host runs many WhatsApp accounts (one *whatsmeow.Client + one
// voip.Manager each) inside a single VoIP worker process.
//
// This is the multi-session half of docs/voip_production.md §4.1; the
// account→worker affinity half is voip/router. A worker hosts the accounts the
// ring assigns to it, enforces a worker-wide active-call cap (calls are
// CPU-bound — MLow + pion per active call), and fans all per-account callbacks
// into single host-level callbacks tagged with the account id.
//
// ⚠️ Same ban-risk rules as the rest of voip/: everything stays behind
// voip.Config.Enabled (default false), disposable accounts only.
package host

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"

	"github.com/williamprado/whatsmeow/voip"
	"github.com/williamprado/whatsmeow/voip/call"
	"github.com/williamprado/whatsmeow/voip/core"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var (
	// ErrNoSession is returned when an action references an account this
	// worker does not host (check the affinity ring — it may live elsewhere).
	ErrNoSession = errors.New("host: no session for that account on this worker")
	// ErrDuplicate is returned when attaching an account that is already hosted.
	ErrDuplicate = errors.New("host: account already hosted")
	// ErrAtCapacity is returned by StartCall when the worker-wide
	// MaxActiveCalls cap is reached. Fail fast — do not degrade live calls.
	ErrAtCapacity = errors.New("host: worker at max active calls")
)

// Config controls a multi-session host.
type Config struct {
	// Manager is the per-account voip.Manager configuration. Its
	// MaxConcurrentCalls caps calls per account; MaxActiveCalls below caps the
	// whole worker.
	Manager voip.Config
	// MaxActiveCalls caps simultaneous active calls across ALL hosted accounts
	// (0 = unlimited). Size it from the CPU-per-call benchmark
	// (docs/voip_production.md §4.2).
	MaxActiveCalls int
}

// Session is one hosted account: its client and its call manager.
type Session struct {
	Account string
	Client  *whatsmeow.Client
	Manager *voip.Manager
}

// Host owns the sessions of one VoIP worker.
type Host struct {
	cfg   Config
	waLog waLog.Logger
	log   *slog.Logger

	mu       sync.RWMutex
	sessions map[string]*Session

	onIncoming  func(account string, c *call.CallInfo)
	onState     func(account string, c *call.CallInfo)
	onEnded     func(account string, c *call.CallInfo)
	onPeerAudio func(account, callID string, pcm16 []float32)
}

// New creates an empty host. waLogger is used for the whatsmeow clients this
// host creates (Add/Restore); log may be nil (slog.Default is used).
func New(cfg Config, waLogger waLog.Logger, log *slog.Logger) *Host {
	if log == nil {
		log = slog.Default()
	}
	if waLogger == nil {
		waLogger = waLog.Noop
	}
	return &Host{
		cfg:      cfg,
		waLog:    waLogger,
		log:      log,
		sessions: make(map[string]*Session),
	}
}

// OnIncomingCall registers the host-level inbound-ring callback. Register the
// callbacks before attaching sessions.
func (h *Host) OnIncomingCall(fn func(account string, c *call.CallInfo)) { h.onIncoming = fn }

// OnCallStateChange registers the host-level state-transition callback.
func (h *Host) OnCallStateChange(fn func(account string, c *call.CallInfo)) { h.onState = fn }

// OnCallEnded registers the host-level call-ended callback.
func (h *Host) OnCallEnded(fn func(account string, c *call.CallInfo)) { h.onEnded = fn }

// OnPeerAudio registers the host-level decoded-peer-PCM callback
// (16 kHz mono float32, tagged with account + call id).
func (h *Host) OnPeerAudio(fn func(account, callID string, pcm16 []float32)) { h.onPeerAudio = fn }

// Attach registers an existing client under the given account id, wires its
// voip.Manager, and starts the manager. It does NOT connect the client — the
// caller owns pairing/connection (useful for QR flows and tests). The account
// id should be the bare JID string (types.JID.ToNonAD().String()).
func (h *Host) Attach(account string, client *whatsmeow.Client) (*Session, error) {
	mgr := voip.New(client, h.cfg.Manager, h.log.With("account", account))
	s := &Session{Account: account, Client: client, Manager: mgr}

	mgr.OnIncomingCall(func(c *call.CallInfo) {
		if h.onIncoming != nil {
			h.onIncoming(account, c)
		}
	})
	mgr.OnCallStateChange(func(c *call.CallInfo) {
		if h.onState != nil {
			h.onState(account, c)
		}
	})
	mgr.OnCallEnded(func(c *call.CallInfo) {
		if h.onEnded != nil {
			h.onEnded(account, c)
		}
	})
	mgr.OnPeerAudio(func(callID string, pcm16 []float32) {
		if h.onPeerAudio != nil {
			h.onPeerAudio(account, callID, pcm16)
		}
	})

	h.mu.Lock()
	if _, dup := h.sessions[account]; dup {
		h.mu.Unlock()
		return nil, ErrDuplicate
	}
	h.sessions[account] = s
	h.mu.Unlock()

	mgr.Start()
	h.log.Info("session attached", "account", account, "hosted", h.Len())
	return s, nil
}

// Add loads the stored device for jid from the container, creates a client,
// attaches it, and connects. The account id is jid.ToNonAD().String().
func (h *Host) Add(ctx context.Context, container *sqlstore.Container, jid types.JID) (*Session, error) {
	device, err := container.GetDevice(ctx, jid)
	if err != nil {
		return nil, err
	}
	if device == nil {
		return nil, ErrNoSession
	}
	client := whatsmeow.NewClient(device, h.waLog)
	s, err := h.Attach(jid.ToNonAD().String(), client)
	if err != nil {
		return nil, err
	}
	if err := client.Connect(); err != nil {
		h.detach(s.Account)
		return nil, err
	}
	return s, nil
}

// Restore attaches and connects every paired device in the container that the
// filter accepts (nil filter = all). Use the affinity ring as the filter so a
// worker only hosts its own accounts:
//
//	h.Restore(ctx, container, func(a string) bool { return ring.Owns(workerID, a) })
//
// Individual failures are logged and skipped; returns the number hosted.
func (h *Host) Restore(ctx context.Context, container *sqlstore.Container, filter func(account string) bool) (int, error) {
	devices, err := container.GetAllDevices(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, device := range devices {
		if device.ID == nil {
			continue
		}
		account := device.ID.ToNonAD().String()
		if filter != nil && !filter(account) {
			continue
		}
		client := whatsmeow.NewClient(device, h.waLog)
		s, err := h.Attach(account, client)
		if err != nil {
			h.log.Warn("restore: attach failed", "account", account, "err", err)
			continue
		}
		if err := client.Connect(); err != nil {
			h.detach(s.Account)
			h.log.Warn("restore: connect failed", "account", account, "err", err)
			continue
		}
		n++
	}
	return n, nil
}

// Get returns the session for an account, if hosted here.
func (h *Host) Get(account string) (*Session, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[account]
	return s, ok
}

// Accounts returns the hosted account ids, sorted.
func (h *Host) Accounts() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.sessions))
	for a := range h.sessions {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// Len returns the number of hosted sessions.
func (h *Host) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.sessions)
}

// ActiveCalls returns the number of active calls across all hosted accounts.
func (h *Host) ActiveCalls() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, s := range h.sessions {
		n += s.Manager.ActiveCalls()
	}
	return n
}

// StartCall places an outbound call from the given account, enforcing the
// worker-wide MaxActiveCalls cap before dialing.
func (h *Host) StartCall(ctx context.Context, account string, peer types.JID, isVideo bool) (string, error) {
	s, ok := h.Get(account)
	if !ok {
		return "", ErrNoSession
	}
	if h.cfg.MaxActiveCalls > 0 && h.ActiveCalls() >= h.cfg.MaxActiveCalls {
		return "", ErrAtCapacity
	}
	return s.Manager.StartCall(ctx, peer, isVideo)
}

// AcceptCall answers a ringing inbound call on the given account, enforcing
// the worker-wide cap (an already-ringing call counts toward it, so the check
// uses > rather than >=).
func (h *Host) AcceptCall(ctx context.Context, account, callID string) error {
	s, ok := h.Get(account)
	if !ok {
		return ErrNoSession
	}
	if h.cfg.MaxActiveCalls > 0 && h.ActiveCalls() > h.cfg.MaxActiveCalls {
		return ErrAtCapacity
	}
	return s.Manager.AcceptCall(ctx, callID)
}

// RejectCall declines a ringing inbound call on the given account.
func (h *Host) RejectCall(ctx context.Context, account, callID string, reason core.EndCallReason) error {
	s, ok := h.Get(account)
	if !ok {
		return ErrNoSession
	}
	return s.Manager.RejectCall(ctx, callID, reason)
}

// EndCall hangs up a call on the given account.
func (h *Host) EndCall(ctx context.Context, account, callID string, reason core.EndCallReason) error {
	s, ok := h.Get(account)
	if !ok {
		return ErrNoSession
	}
	return s.Manager.EndCall(ctx, callID, reason)
}

// FeedCapturedPCM pushes captured 16 kHz mono float32 PCM into a call on the
// given account.
func (h *Host) FeedCapturedPCM(account, callID string, pcm16 []float32) error {
	s, ok := h.Get(account)
	if !ok {
		return ErrNoSession
	}
	return s.Manager.FeedCapturedPCM(callID, pcm16)
}

// Remove stops an account's manager (ending its calls), disconnects its
// client, and drops the session. Used when the affinity ring moves an account
// to another worker.
func (h *Host) Remove(account string) error {
	s, ok := h.Get(account)
	if !ok {
		return ErrNoSession
	}
	s.Manager.Stop()
	s.Client.Disconnect()
	h.detach(account)
	h.log.Info("session removed", "account", account, "hosted", h.Len())
	return nil
}

// Shutdown gracefully stops the worker: every manager is stopped (ending its
// calls with user-ended) and every client disconnected. The host is empty and
// reusable afterwards, but a draining deploy should stop routing to this
// worker first (docs/voip_production.md §4.3).
func (h *Host) Shutdown() {
	h.mu.Lock()
	all := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		all = append(all, s)
	}
	h.sessions = make(map[string]*Session)
	h.mu.Unlock()

	for _, s := range all {
		s.Manager.Stop()
		s.Client.Disconnect()
	}
	h.log.Info("host shut down", "sessions", len(all))
}

func (h *Host) detach(account string) {
	h.mu.Lock()
	delete(h.sessions, account)
	h.mu.Unlock()
}
