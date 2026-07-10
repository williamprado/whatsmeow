// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package voip is the foundation (Phase 0) for WhatsApp voice-call support in
// this fork. It mirrors the architecture of the reference implementation
// (williamprado/WaCalls, module "wacalls", commit
// fc7b8c32c96b10710cc5325c312546f2778f7d97).
//
// Phase 0 scope — RECEIVE ONLY, NO AUDIO:
//   - Detect incoming calls via whatsmeow's public call events and expose them
//     (caller, callID, timestamp).
//   - Reject / terminate an incoming call cleanly (plaintext <call> stanzas).
//   - A VoipSocket adapter over Client.DangerousInternals() — the seam the
//     reference uses — with the cryptographic/offer-send methods STUBBED until
//     Phase 1. The whole thing is gated by Config.Enabled (default false).
//
// ⚠️ EXPERIMENTAL — HIGH ACCOUNT-BAN RISK. VoIP via an unofficial library uses
// reverse-engineered crypto and protocol constants; it is far more sensitive
// than interactive messages. Only ever enable on a DISPOSABLE test account,
// never the production account. No live audio is performed in Phase 0.
//
// WaCalls cannot be added as a normal go.mod dependency: its module path is the
// non-canonical "wacalls" and all of its call logic lives under internal/, so it
// is not importable from this module. Phase 1 will VENDOR/LIFT the relevant
// packages from the pinned commit above rather than import them. See
// docs/voip_foundation.md.
package voip

import (
	"context"
	"errors"
	"fmt"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

// ErrVoIPPhase1 is returned by the cryptographic VoipSocket methods that are
// intentionally not implemented in Phase 0 (offer/accept call-key encryption and
// decryption). They arrive with Phase 1.
var ErrVoIPPhase1 = errors.New("voip: call-key crypto is not implemented in Phase 0 (Phase 1)")

// clientAPI is the small slice of *whatsmeow.Client behaviour the adapter needs.
// It is an interface so the adapter (and the Manager) can be unit-tested without
// a live client. In production it is satisfied by *whatsmeow.Client +
// Client.DangerousInternals().
type clientAPI interface {
	OwnPN() types.JID
	OwnLID() types.JID
	SendNode(ctx context.Context, node waBinary.Node) error
	WaitResponse(reqID string) chan *waBinary.Node
	CancelResponse(reqID string, ch chan *waBinary.Node)
	MakeDeviceIdentityNode() waBinary.Node
	GetUSyncDevices(ctx context.Context, jids []types.JID) ([]types.JID, error)
	ResolveLIDForPN(ctx context.Context, pn types.JID) types.JID
	GetPrivacyToken(ctx context.Context, jid types.JID) ([]byte, error)
}

// VoipSocket is the seam between the (future, lifted) call logic and whatsmeow,
// mirroring core.VoipSocket in the reference. Phase 0 implements the plaintext
// plumbing (send/query/identity/usync/lid/token); the call-key crypto methods
// return ErrVoIPPhase1 until Phase 1.
type VoipSocket interface {
	OwnPN() types.JID
	OwnLID() types.JID
	AccountDeviceIdentityNode() (waBinary.Node, bool)
	SendNode(ctx context.Context, node waBinary.Node) error
	Query(ctx context.Context, node waBinary.Node) (*waBinary.Node, error)
	GetUSyncDevices(ctx context.Context, jids []types.JID) ([]types.JID, error)
	AssertSessions(ctx context.Context, jids []types.JID, force bool) error
	CreateParticipantNodes(ctx context.Context, devices []types.JID, callKey []byte, encAttrs waBinary.Attrs) ([]waBinary.Node, bool, error)
	DecryptCallKey(ctx context.Context, from types.JID, encChild *waBinary.Node) ([]byte, error)
	GetTCToken(ctx context.Context, jid types.JID) ([]byte, error)
	ResolveLIDForPN(ctx context.Context, pn types.JID) types.JID
}

// socket adapts a clientAPI (backed by Client.DangerousInternals()) to VoipSocket.
type socket struct {
	api clientAPI
}

var _ VoipSocket = (*socket)(nil)

func (s *socket) OwnPN() types.JID  { return s.api.OwnPN() }
func (s *socket) OwnLID() types.JID { return s.api.OwnLID() }

func (s *socket) AccountDeviceIdentityNode() (waBinary.Node, bool) {
	return s.api.MakeDeviceIdentityNode(), true
}

func (s *socket) SendNode(ctx context.Context, node waBinary.Node) error {
	return s.api.SendNode(ctx, node)
}

// Query sends a node and waits for the response correlated by the stanza id.
func (s *socket) Query(ctx context.Context, node waBinary.Node) (*waBinary.Node, error) {
	id, _ := node.Attrs["id"].(string)
	if id == "" {
		return nil, fmt.Errorf("voip: Query node has no string id attribute")
	}
	ch := s.api.WaitResponse(id)
	if err := s.api.SendNode(ctx, node); err != nil {
		s.api.CancelResponse(id, ch)
		return nil, err
	}
	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		s.api.CancelResponse(id, ch)
		return nil, ctx.Err()
	}
}

func (s *socket) GetUSyncDevices(ctx context.Context, jids []types.JID) ([]types.JID, error) {
	return s.api.GetUSyncDevices(ctx, jids)
}

func (s *socket) ResolveLIDForPN(ctx context.Context, pn types.JID) types.JID {
	return s.api.ResolveLIDForPN(ctx, pn)
}

func (s *socket) GetTCToken(ctx context.Context, jid types.JID) ([]byte, error) {
	return s.api.GetPrivacyToken(ctx, jid)
}

// AssertSessions is a no-op (matches the reference adapter); session
// establishment is handled lazily during per-device encryption in Phase 1.
func (s *socket) AssertSessions(ctx context.Context, jids []types.JID, force bool) error {
	return nil
}

// CreateParticipantNodes (Phase 1) — Signal-encrypts the call key per device.
func (s *socket) CreateParticipantNodes(ctx context.Context, devices []types.JID, callKey []byte, encAttrs waBinary.Attrs) ([]waBinary.Node, bool, error) {
	return nil, false, ErrVoIPPhase1
}

// DecryptCallKey (Phase 1) — Signal-decrypts the peer's call key.
func (s *socket) DecryptCallKey(ctx context.Context, from types.JID, encChild *waBinary.Node) ([]byte, error) {
	return nil, ErrVoIPPhase1
}
