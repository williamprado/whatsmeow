// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
)

// ============================================================================
// CUSTOM FORK PATCH — interactive button node routing
// ============================================================================
//
// Upstream whatsmeow only attaches the plaintext <biz> routing node for
// ButtonsMessage and ListMessage (see getButtonTypeFromMessage /
// getButtonAttributes in send.go). TemplateMessage and InteractiveMessage
// (native flow + carousel) are sent with NO <biz> node, so the server has no
// routing hint that the encrypted payload contains interactive buttons.
//
// This file adds that routing hint for those two types WITHOUT rewriting the
// upstream functions: send.go calls customButtonType / customButtonAttributes
// as a guarded first step (three commented lines each), and all the actual
// logic lives here. That keeps the merge-conflict surface against the
// upstream-sync automation to a minimum and makes the customization obvious.
//
// Node shape produced (matches what official WhatsApp Web clients send):
//
//	<biz>
//	  <interactive type="native_flow" v="1"/>
//	</biz>
//
// The button definitions themselves travel inside the *encrypted*
// InteractiveMessage / TemplateMessage protobuf — this node is only the
// cleartext routing hint the server uses to classify the message.
//
// ⚠️ EXPERIMENTAL: whether a recipient actually renders the buttons is gated by
// WhatsApp server-side (historically only official WhatsApp Business API senders
// are allowed to render interactive buttons). This patch makes whatsmeow emit
// the correct node; it cannot lift that server-side gating. See
// docs/interactive_messages.md.

// interactiveNodeVersion is the "v" attribute on the <interactive> routing node.
const interactiveNodeVersion = "1"

// customButtonType returns the <biz> child tag for the interactive message types
// that upstream getButtonTypeFromMessage does not handle. The bool is false when
// this patch does not apply (so the upstream switch runs unchanged).
//
// It does NOT unwrap ViewOnce/Ephemeral itself: it is called at the top of the
// upstream getButtonTypeFromMessage, which already recurses through those
// wrappers and re-enters this hook on the unwrapped message.
func customButtonType(msg *waE2E.Message) (string, bool) {
	switch {
	case msg.InteractiveMessage != nil:
		// Both NativeFlowMessage and CarouselMessage are carried as an
		// InteractiveMessage and route through the same <interactive> node
		// (carousel cards are themselves native-flow interactive messages).
		return "interactive", true
	case msg.TemplateMessage != nil:
		// Hydrated four-row template buttons. Best-effort: route under the same
		// <interactive>/native_flow hint. Documented as experimental — if the
		// server rejects templates with this hint, revert this case.
		return "interactive", true
	default:
		return "", false
	}
}

// customButtonAttributes returns the attrs for the <biz> child node for the
// types customButtonType handles. The bool is false when this patch does not
// apply.
func customButtonAttributes(msg *waE2E.Message) (waBinary.Attrs, bool) {
	switch {
	case msg.InteractiveMessage != nil, msg.TemplateMessage != nil:
		return waBinary.Attrs{
			"v":    interactiveNodeVersion,
			"type": "native_flow",
		}, true
	default:
		return nil, false
	}
}

// customInteractiveBizNode builds the full nested <biz> node that official
// WhatsApp Web clients send for native-flow interactive messages:
//
//	<biz>
//	  <interactive type="native_flow" v="1">
//	    <native_flow name="..." v="2"/>
//	  </interactive>
//	</biz>
//
// The single-level form (interactive node with no native_flow child) is rejected
// by the server with error 479, so this adds the grandchild. The "name" hints at
// the button kind ("mixed" when the message mixes kinds, otherwise the single
// kind). The bool is false when this patch does not apply (getMessageContent
// then builds the upstream single-child node).
func customInteractiveBizNode(msg *waE2E.Message) (*waBinary.Node, bool) {
	unwrapped := unwrapForButtons(msg)
	if unwrapped.InteractiveMessage == nil && unwrapped.TemplateMessage == nil {
		return nil, false
	}
	nativeFlow := waBinary.Node{
		Tag: "native_flow",
		Attrs: waBinary.Attrs{
			"name": nativeFlowName(unwrapped),
			"v":    "2",
		},
	}
	return &waBinary.Node{
		Tag: "biz",
		Content: []waBinary.Node{{
			Tag: "interactive",
			Attrs: waBinary.Attrs{
				"type": "native_flow",
				"v":    interactiveNodeVersion,
			},
			Content: []waBinary.Node{nativeFlow},
		}},
	}, true
}

// unwrapForButtons peels the ViewOnce/Ephemeral wrappers the upstream button
// helpers also unwrap, so this patch sees the inner message.
func unwrapForButtons(msg *waE2E.Message) *waE2E.Message {
	switch {
	case msg.ViewOnceMessage != nil:
		return unwrapForButtons(msg.ViewOnceMessage.Message)
	case msg.ViewOnceMessageV2 != nil:
		return unwrapForButtons(msg.ViewOnceMessageV2.Message)
	case msg.EphemeralMessage != nil:
		return unwrapForButtons(msg.EphemeralMessage.Message)
	default:
		return msg
	}
}

// nativeFlowName returns the <native_flow> "name" attribute. If every native
// flow button shares one name it is used; otherwise "mixed".
func nativeFlowName(msg *waE2E.Message) string {
	nf := msg.GetInteractiveMessage().GetNativeFlowMessage()
	if nf == nil || len(nf.GetButtons()) == 0 {
		return "mixed"
	}
	name := nf.GetButtons()[0].GetName()
	for _, b := range nf.GetButtons()[1:] {
		if b.GetName() != name {
			return "mixed"
		}
	}
	return name
}
