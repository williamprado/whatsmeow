// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package bridge carries raw PCM audio between a browser (a human agent) and a
// WhatsApp call over a WebRTC data channel. The call core only ever deals in
// []float32 PCM (via voip.Manager.FeedCapturedPCM / OnPeerAudio), so it stays
// unaware of this transport.
//
// Ported from the reference williamprado/WaCalls (cmd/server/bridge.go). Used to
// connect a WhatsApp voice call to a human operator in the browser.
package bridge

import (
	"log/slog"
	"sync/atomic"

	"github.com/pion/webrtc/v4"

	"github.com/williamprado/whatsmeow/voip/media"
)

// PCMChannelLabel is the data channel the browser opens to carry raw 16 kHz mono
// Int16 LE PCM in both directions. The browser side must create it with this label.
const PCMChannelLabel = "pcm"

// Bridge is the browser-leg adapter: it carries raw PCM between the browser and
// the call over a WebRTC data channel.
type Bridge struct {
	pc  *webrtc.PeerConnection
	dc  atomic.Pointer[webrtc.DataChannel]
	log *slog.Logger

	// OnBrowserPCM is invoked with 16 kHz mono PCM captured from the browser mic.
	// Wire it to voip.Manager.FeedCapturedPCM(callID, pcm).
	OnBrowserPCM func(pcm []float32)
	// OnTerminalICE fires when the peer connection fails or closes.
	OnTerminalICE func()
}

// New answers the browser's SDP offer, sets up the PCM data channel, and returns
// the Bridge plus the SDP answer to hand back to the browser. iceServers are the
// STUN/TURN servers for the browser leg's NAT traversal (nil is fine on
// localhost/LAN, but production needs at least STUN and usually TURN).
func New(offerSDP string, iceServers []webrtc.ICEServer, log *slog.Logger) (*Bridge, string, error) {
	if log == nil {
		log = slog.Default()
	}
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: iceServers})
	if err != nil {
		return nil, "", err
	}
	br := &Bridge{pc: pc, log: log}

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != PCMChannelLabel {
			return
		}
		br.dc.Store(dc)
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			if cb := br.OnBrowserPCM; cb != nil && len(msg.Data) > 0 {
				cb(media.PCMInt16LEToFloat32(msg.Data))
			}
		})
	})

	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		br.log.Debug("browser ice state", "state", s.String())
		if s == webrtc.ICEConnectionStateFailed || s == webrtc.ICEConnectionStateClosed {
			if br.OnTerminalICE != nil {
				br.OnTerminalICE()
			}
		}
	})

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}); err != nil {
		pc.Close()
		return nil, "", err
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		return nil, "", err
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		return nil, "", err
	}
	<-gatherComplete

	return br, pc.LocalDescription().SDP, nil
}

// WritePCM sends 16 kHz mono float32 PCM (the WhatsApp peer's audio) to the
// browser as Int16 LE over the data channel. No-op until the channel is open.
// Wire it from voip.Manager.OnPeerAudio.
func (b *Bridge) WritePCM(pcm []float32) error {
	dc := b.dc.Load()
	if dc == nil || len(pcm) == 0 {
		return nil
	}
	return dc.Send(media.PCMFloat32ToInt16LE(pcm))
}

// Close tears down the peer connection.
func (b *Bridge) Close() {
	if b.pc != nil {
		_ = b.pc.Close()
	}
}
