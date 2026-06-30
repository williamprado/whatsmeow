// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow/types"
)

// ResolveRecipientJID resolves a phone number to its canonical WhatsApp JID by
// querying the server with IsOnWhatsApp.
//
// SendMessage delivers to the exact JID it is given and does not normalize phone
// numbers. For some accounts the dialed number differs from the registered JID —
// most notably the Brazilian mobile "9th digit", where e.g. 5577988272902
// (with the 9) is registered as 557788272902 (without it). Sending to the
// non-canonical JID is accepted by the server (a message ID is returned) but
// delivered to nobody. Resolving the JID first avoids that silent black hole.
//
// phone is the number in international format, digits only (no "+"), e.g.
// "5577988272902". It returns an error if the number is not registered on
// WhatsApp.
func (cli *Client) ResolveRecipientJID(ctx context.Context, phone string) (types.JID, error) {
	resp, err := cli.IsOnWhatsApp(ctx, []string{phone})
	if err != nil {
		return types.EmptyJID, fmt.Errorf("IsOnWhatsApp query failed for %s: %w", phone, err)
	}
	for _, r := range resp {
		if r.Query == phone {
			if !r.IsIn || r.JID.IsEmpty() {
				return types.EmptyJID, fmt.Errorf("%s is not registered on WhatsApp", phone)
			}
			return r.JID, nil
		}
	}
	return types.EmptyJID, fmt.Errorf("no IsOnWhatsApp result for %s", phone)
}
