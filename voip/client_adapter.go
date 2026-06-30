// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package voip

import (
	"context"

	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

// clientAdapter implements clientAPI over a *whatsmeow.Client, using the
// Client.DangerousInternals() facade for the low-level node/identity operations
// and the public Client/Store API for the rest. This is the only file that wires
// the voip package to whatsmeow's internals.
type clientAdapter struct {
	cli *whatsmeow.Client
	di  *whatsmeow.DangerousInternalClient
}

func newClientAdapter(cli *whatsmeow.Client) *clientAdapter {
	return &clientAdapter{cli: cli, di: cli.DangerousInternals()}
}

func (c *clientAdapter) OwnPN() types.JID  { return c.di.GetOwnID() }
func (c *clientAdapter) OwnLID() types.JID { return c.di.GetOwnLID() }

func (c *clientAdapter) SendNode(ctx context.Context, node waBinary.Node) error {
	return c.di.SendNode(ctx, node)
}

func (c *clientAdapter) WaitResponse(reqID string) chan *waBinary.Node {
	return c.di.WaitResponse(reqID)
}

func (c *clientAdapter) CancelResponse(reqID string, ch chan *waBinary.Node) {
	c.di.CancelResponse(reqID, ch)
}

func (c *clientAdapter) MakeDeviceIdentityNode() waBinary.Node {
	return c.di.MakeDeviceIdentityNode()
}

func (c *clientAdapter) GetUSyncDevices(ctx context.Context, jids []types.JID) ([]types.JID, error) {
	return c.cli.GetUserDevices(ctx, jids)
}

func (c *clientAdapter) ResolveLIDForPN(ctx context.Context, pn types.JID) types.JID {
	if lid, err := c.cli.Store.LIDs.GetLIDForPN(ctx, pn); err == nil && !lid.IsEmpty() {
		return lid
	}
	if info, err := c.cli.GetUserInfo(ctx, []types.JID{pn}); err == nil {
		return info[pn].LID
	}
	return types.EmptyJID
}

func (c *clientAdapter) GetPrivacyToken(ctx context.Context, jid types.JID) ([]byte, error) {
	tok, err := c.cli.Store.PrivacyTokens.GetPrivacyToken(ctx, jid)
	if err != nil || tok == nil {
		return nil, err
	}
	return tok.Token, nil
}

// NewVoipSocket builds the VoipSocket adapter over a *whatsmeow.Client.
func NewVoipSocket(cli *whatsmeow.Client) VoipSocket {
	return &socket{api: newClientAdapter(cli)}
}
