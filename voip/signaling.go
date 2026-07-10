// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package voip

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

// ============================================================================
// Call signaling — <offer> / <accept> build + relay-ack parse
// ============================================================================
//
// Ported faithfully from the reference (williamprado/WaCalls,
// internal/voip/signaling). Several values below are REVERSE-ENGINEERED WhatsApp
// protocol constants — they have no official spec and WhatsApp can change them
// server-side. See docs/voip_phase1.md for the full list and risk note.

// capabilityOffer is the reverse-engineered <capability ver="1"> blob in <offer>.
var capabilityOffer = []byte{0x01, 0x05, 0xf7, 0x09, 0xe4, 0xbb, 0x07}

// BuildOfferStanza builds the <call><offer> stanza to start a 1:1 call: it USyncs
// the peer's devices, Signal-encrypts the call key per device into
// <destination>, and assembles the offer (audio codecs, net, capability, encopt,
// privacy token, device-identity). Audio-only (isVideo=false) for now.
func BuildOfferStanza(ctx context.Context, sock VoipSocket, callID string, callKey []byte, peerJid types.JID, isVideo bool) (waBinary.Node, error) {
	creator := sock.OwnLID()
	if creator.IsEmpty() {
		creator = sock.OwnPN()
	}

	rawDevices, err := sock.GetUSyncDevices(ctx, []types.JID{peerJid})
	if err != nil {
		return waBinary.Node{}, fmt.Errorf("usync devices: %w", err)
	}
	if err := sock.AssertSessions(ctx, rawDevices, false); err != nil {
		return waBinary.Node{}, fmt.Errorf("assert sessions: %w", err)
	}

	destinations, includeDeviceIdentity, err := sock.CreateParticipantNodes(ctx, rawDevices, callKey, waBinary.Attrs{"count": "0"})
	if err != nil {
		return waBinary.Node{}, fmt.Errorf("participant nodes: %w", err)
	}

	var offerContent []waBinary.Node
	if token, err := sock.GetTCToken(ctx, peerJid.ToNonAD()); err == nil && len(token) > 0 {
		offerContent = append(offerContent, waBinary.Node{Tag: "privacy", Content: token})
	}
	offerContent = append(offerContent,
		waBinary.Node{Tag: "audio", Attrs: waBinary.Attrs{"enc": "opus", "rate": "8000"}},
		waBinary.Node{Tag: "audio", Attrs: waBinary.Attrs{"enc": "opus", "rate": "16000"}},
	)
	if isVideo {
		offerContent = append(offerContent, waBinary.Node{Tag: "video", Attrs: waBinary.Attrs{
			"enc": "vp8", "dec": "vp8", "orientation": "0",
			"screen_width": "1920", "screen_height": "1080", "device_orientation": "0",
		}})
	}
	offerContent = append(offerContent,
		waBinary.Node{Tag: "net", Attrs: waBinary.Attrs{"medium": "3"}},
		waBinary.Node{Tag: "capability", Attrs: waBinary.Attrs{"ver": "1"}, Content: capabilityOffer},
		waBinary.Node{Tag: "destination", Content: destinations},
		waBinary.Node{Tag: "encopt", Attrs: waBinary.Attrs{"keygen": "2"}},
	)
	if includeDeviceIdentity {
		if di, ok := sock.AccountDeviceIdentityNode(); ok {
			offerContent = append(offerContent, di)
		}
	}

	return waBinary.Node{
		Tag:   "call",
		Attrs: waBinary.Attrs{"to": peerJid, "id": generateStanzaID()},
		Content: []waBinary.Node{{
			Tag:     "offer",
			Attrs:   waBinary.Attrs{"call-id": callID, "call-creator": creator},
			Content: offerContent,
		}},
	}, nil
}

// BuildAcceptStanza builds the <call><accept> stanza to answer an offer: it
// re-encrypts the call key for the call creator and assembles the accept node.
func BuildAcceptStanza(ctx context.Context, sock VoipSocket, callID string, callKey []byte, peerJid, callCreator types.JID, isVideo bool) (waBinary.Node, error) {
	if err := sock.AssertSessions(ctx, []types.JID{callCreator}, true); err != nil {
		return waBinary.Node{}, fmt.Errorf("assert creator session: %w", err)
	}

	nodes, includeDeviceIdentity, err := sock.CreateParticipantNodes(ctx, []types.JID{callCreator}, callKey, waBinary.Attrs{"count": "0"})
	if err != nil {
		return waBinary.Node{}, fmt.Errorf("encrypt accept: %w", err)
	}
	encNode := extractEncFromParticipant(nodes)
	if encNode == nil {
		return waBinary.Node{}, fmt.Errorf("no enc node produced for accept")
	}

	acceptContent := []waBinary.Node{
		{Tag: "audio", Attrs: waBinary.Attrs{"enc": "opus", "rate": "16000"}},
		{Tag: "net", Attrs: waBinary.Attrs{"medium": "3"}},
		*encNode,
		{Tag: "encopt", Attrs: waBinary.Attrs{"keygen": "2"}},
	}
	if includeDeviceIdentity {
		if di, ok := sock.AccountDeviceIdentityNode(); ok {
			acceptContent = append(acceptContent, di)
		}
	}
	if isVideo {
		acceptContent = append(acceptContent, waBinary.Node{Tag: "video", Attrs: waBinary.Attrs{"enc": "vp8"}})
	}

	return waBinary.Node{
		Tag:   "call",
		Attrs: waBinary.Attrs{"to": peerJid.ToNonAD(), "id": generateStanzaID()},
		Content: []waBinary.Node{{
			Tag:     "accept",
			Attrs:   waBinary.Attrs{"call-id": callID, "call-creator": callCreator},
			Content: acceptContent,
		}},
	}, nil
}

func extractEncFromParticipant(nodes []waBinary.Node) *waBinary.Node {
	for _, n := range nodes {
		n := n
		if n.Tag == "enc" {
			return &n
		}
		for _, c := range nodeChildren(&n) {
			c := c
			if c.Tag == "enc" {
				return &c
			}
		}
	}
	return nil
}

// RelayEndpoint is a single WhatsApp relay address parsed from the offer ack.
type RelayEndpoint struct {
	IP           string
	Port         int
	Token        string
	AuthToken    string
	RawAuthToken []byte
	RawToken     []byte
	Key          string
	RelayID      int
	Protocol     int
	C2RRtt       *int
	RelayName    string
	AddressBytes []byte
	AuthTokenID  string
}

// ParsedRelayAck is the parsed content of the synchronous offer ack.
type ParsedRelayAck struct {
	Relays          []RelayEndpoint
	ParticipantJids []string
	UUID            string
	SelfPid         *int
	PeerPid         *int
	HbhKey          []byte
}

// ParseRelayFromAck parses the <ack>/<relay> node returned for an offer into the
// relay endpoints, participant JIDs, pids and hop-by-hop key.
func ParseRelayFromAck(ackNode *waBinary.Node) ParsedRelayAck {
	res := ParsedRelayAck{}
	participantSeen := map[string]bool{}
	addParticipant := func(jid string) {
		if jid != "" && !participantSeen[jid] {
			participantSeen[jid] = true
			res.ParticipantJids = append(res.ParticipantJids, jid)
		}
	}

	for _, child := range nodeChildren(ackNode) {
		child := child
		if child.Tag == "user" {
			for _, deviceNode := range nodeChildren(&child) {
				if deviceNode.Tag == "device" && hasAttr(deviceNode.Attrs, "jid") {
					addParticipant(attrString(deviceNode.Attrs, "jid"))
				}
			}
		}
		if child.Tag != "relay" {
			continue
		}

		res.UUID = attrString(child.Attrs, "uuid")
		if hasAttr(child.Attrs, "self_pid") {
			v := attrInt(child.Attrs, "self_pid", 0)
			res.SelfPid = &v
		}
		if hasAttr(child.Attrs, "peer_pid") {
			v := attrInt(child.Attrs, "peer_pid", 0)
			res.PeerPid = &v
		}

		relayContent := nodeChildren(&child)
		for _, rc := range relayContent {
			if rc.Tag == "participant" && hasAttr(rc.Attrs, "jid") {
				addParticipant(attrString(rc.Attrs, "jid"))
			}
		}

		var relayKey string
		tokens := map[string]string{}
		authTokens := map[string]string{}
		rawTokens := map[string][]byte{}
		rawAuthTokens := map[string][]byte{}

		for _, rc := range relayContent {
			rc := rc
			switch rc.Tag {
			case "key":
				if b := nodeBytes(&rc); b != nil {
					relayKey = string(b)
				}
			case "hbh_key":
				if b := nodeBytes(&rc); b != nil {
					switch {
					case len(b) == 30:
						res.HbhKey = b
					case len(b) > 30:
						if decoded, err := base64.StdEncoding.DecodeString(string(b)); err == nil && len(decoded) == 30 {
							res.HbhKey = decoded
						}
					}
				}
			case "token":
				if b := nodeBytes(&rc); b != nil {
					id := attrStringOr(rc.Attrs, "id", "0")
					tokens[id] = base64.StdEncoding.EncodeToString(b)
					rawTokens[id] = b
				}
			case "auth_token":
				if b := nodeBytes(&rc); b != nil {
					id := attrStringOr(rc.Attrs, "id", "0")
					authTokens[id] = base64.StdEncoding.EncodeToString(b)
					rawAuthTokens[id] = b
				}
			}
		}

		for _, rc := range relayContent {
			rc := rc
			if rc.Tag != "te2" {
				continue
			}
			addrBytes := nodeBytes(&rc)
			if len(addrBytes) < 6 {
				continue
			}
			tokenID := attrStringOr(rc.Attrs, "token_id", "0")
			authTokenID := attrString(rc.Attrs, "auth_token_id")
			ep := RelayEndpoint{
				Token:        tokens[tokenID],
				RawToken:     rawTokens[tokenID],
				Key:          relayKey,
				RelayID:      attrInt(rc.Attrs, "relay_id", 0),
				Protocol:     attrInt(rc.Attrs, "protocol", 0),
				RelayName:    attrString(rc.Attrs, "relay_name"),
				AddressBytes: append([]byte(nil), addrBytes...),
			}
			if authTokenID != "" {
				ep.AuthToken = authTokens[authTokenID]
				ep.RawAuthToken = rawAuthTokens[authTokenID]
				ep.AuthTokenID = authTokenID
			} else {
				ep.AuthTokenID = tokenID
			}
			if hasAttr(rc.Attrs, "c2r_rtt") {
				v := attrInt(rc.Attrs, "c2r_rtt", 0)
				ep.C2RRtt = &v
			}
			if len(addrBytes) == 6 {
				ep.IP = ipv4String(addrBytes[:4])
				ep.Port = int(addrBytes[4])<<8 | int(addrBytes[5])
				res.Relays = append(res.Relays, ep)
			}
		}
	}

	sortRelaysByRtt(res.Relays)
	return res
}

// --- node/attr helpers (mirroring the reference wanode package) ---

func nodeChildren(n *waBinary.Node) []waBinary.Node {
	if n == nil {
		return nil
	}
	if children, ok := n.Content.([]waBinary.Node); ok {
		return children
	}
	return nil
}

func nodeBytes(n *waBinary.Node) []byte {
	if n == nil {
		return nil
	}
	if b, ok := n.Content.([]byte); ok {
		return b
	}
	return nil
}

func attrString(attrs waBinary.Attrs, key string) string {
	v, ok := attrs[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprintf("%v", t)
	}
}

func attrInt(attrs waBinary.Attrs, key string, fallback int) int {
	s := attrString(attrs, key)
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

func hasAttr(attrs waBinary.Attrs, key string) bool {
	v, ok := attrs[key]
	return ok && v != nil && attrString(attrs, key) != ""
}

func attrStringOr(attrs waBinary.Attrs, key, fallback string) string {
	if s := attrString(attrs, key); s != "" {
		return s
	}
	return fallback
}

func ipv4String(b []byte) string {
	return strconv.Itoa(int(b[0])) + "." + strconv.Itoa(int(b[1])) + "." +
		strconv.Itoa(int(b[2])) + "." + strconv.Itoa(int(b[3]))
}

func sortRelaysByRtt(relays []RelayEndpoint) {
	sort.SliceStable(relays, func(i, j int) bool {
		ri, rj := relays[i].C2RRtt, relays[j].C2RRtt
		switch {
		case ri == nil && rj == nil:
			return false
		case ri == nil:
			return false
		case rj == nil:
			return true
		default:
			return *ri < *rj
		}
	})
}
