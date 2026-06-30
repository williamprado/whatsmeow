// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	"context"
	"encoding/hex"

	"go.mau.fi/util/random"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
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

// nativeFlowVersion is the "v" attribute on the inner <native_flow> node. This
// matches the value the reference Baileys implementation (rsalcara/InfiniteAPI,
// src/Socket/messages-send.ts) sends; "9" — not "2" — is required for the
// recipient's Web/Desktop client to render lists and CTA-only buttons.
const nativeFlowVersion = "9"

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
//	    <native_flow name="..." v="9"/>
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
			"v":    nativeFlowVersion,
		},
	}
	bizContent := []waBinary.Node{{
		Tag: "interactive",
		Attrs: waBinary.Attrs{
			"type": "native_flow",
			"v":    interactiveNodeVersion,
		},
		Content: []waBinary.Node{nativeFlow},
	}}
	// Carousel needs an extra <quality_control> sibling node inside <biz> for the
	// recipient to render the cards (per the reference implementation). The bot
	// node is still omitted for carousel (handled by relocateInteractiveBizAndAddBot
	// via isCarouselOrCatalog).
	if isCarouselMessage(unwrapped) {
		bizContent = append(bizContent, qualityControlNode())
	}
	return &waBinary.Node{Tag: "biz", Content: bizContent}, true
}

// qualityControlNode builds the carousel quality-control node:
//
//	<quality_control decision_id="<20 random bytes, hex>">
//	  <decision_source value="df"/>
//	</quality_control>
func qualityControlNode() waBinary.Node {
	return waBinary.Node{
		Tag:   "quality_control",
		Attrs: waBinary.Attrs{"decision_id": hex.EncodeToString(random.Bytes(20))},
		Content: []waBinary.Node{{
			Tag:   "decision_source",
			Attrs: waBinary.Attrs{"value": "df"},
		}},
	}
}

// isCarouselMessage reports whether the (already unwrapped) message is an
// InteractiveMessage carousel.
func isCarouselMessage(msg *waE2E.Message) bool {
	return msg.GetInteractiveMessage().GetCarouselMessage() != nil
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

// nativeFlowName returns the <native_flow> "name" attribute, matching the map
// used by the reference Baileys implementation (rsalcara/InfiniteAPI). Only a
// few special flows get a dedicated name; everything else (including plain
// quick_reply and cta_url buttons) is "mixed". The name is NOT inferred from the
// first button outside this map.
//
//	review_and_pay / payment_info -> payment_info
//	mpm                           -> mpm
//	review_order                  -> order_details
//	(anything else)               -> mixed
func nativeFlowName(msg *waE2E.Message) string {
	nf := msg.GetInteractiveMessage().GetNativeFlowMessage()
	for _, b := range nf.GetButtons() {
		switch b.GetName() {
		case "review_and_pay", "payment_info":
			return "payment_info"
		case "mpm":
			return "mpm"
		case "review_order":
			return "order_details"
		}
	}
	return "mixed"
}

// ============================================================================
// CUSTOM FORK PATCH — <bot biz_bot="1"/> node for private 1:1 interactive msgs
// ============================================================================
//
// The reference Baileys implementation (rsalcara/InfiniteAPI,
// src/Socket/messages-send.ts) appends, for private 1:1 interactive messages,
// TWO cleartext nodes at the very end of the stanza, in this order:
//
//	... -> device-identity -> tctoken -> biz -> bot
//
// where <bot biz_bot="1"/> follows the <biz> node. Per the comment in that
// implementation, the bot node is currently REQUIRED for CTA-only and list
// messages to render on Web/Desktop. It is injected for every private 1:1
// interactive message (quick_reply, CTA, list) EXCEPT carousel and catalog.
//
// whatsmeow builds the <biz> node inside getMessageContent, which runs BEFORE
// the tctoken is appended in sendDM — so by default <biz> ends up before
// <tctoken>. relocateInteractiveBizAndAddBot (called from sendDM after the
// tctoken block) moves <biz> to the end and appends <bot> right after it, so the
// final ordering matches the reference exactly.

// isPrivateInteractiveRecipient reports whether `to` is a 1:1 user JID
// (phone number / LID / legacy @c.us) — the only recipients that get the bot
// node. Groups, newsletters, broadcast/status are excluded.
func isPrivateInteractiveRecipient(to types.JID) bool {
	switch to.Server {
	case types.DefaultUserServer, types.HiddenUserServer, types.LegacyUserServer:
		return true
	default:
		return false
	}
}

// isCarouselOrCatalog reports whether the (unwrapped) message is a carousel or
// catalog/shop/product interactive message — the reference excludes these from
// the bot node.
func isCarouselOrCatalog(msg *waE2E.Message) bool {
	m := unwrapForButtons(msg)
	if im := m.GetInteractiveMessage(); im != nil {
		if im.GetCarouselMessage() != nil ||
			im.GetShopStorefrontMessage() != nil ||
			im.GetCollectionMessage() != nil {
			return true
		}
	}
	return m.GetProductMessage() != nil
}

// relocateInteractiveBizAndAddBot moves the <biz> child that getMessageContent
// appended to the end of the stanza (so it lands after <tctoken>) and, unless
// the message is a carousel/catalog, appends a <bot biz_bot="1"/> node right
// after it. It is a no-op for non-1:1 recipients and non-interactive messages.
//
// Called from sendDM after the tctoken/cstoken block. The final biz/bot subtree
// is logged at debug level for auditing.
func (cli *Client) relocateInteractiveBizAndAddBot(node *waBinary.Node, to types.JID, msg *waE2E.Message) {
	if node == nil || !isPrivateInteractiveRecipient(to) {
		return
	}
	if getButtonTypeFromMessage(msg) == "" {
		return // not an interactive/button message
	}
	children := node.GetChildren()
	newContent := make([]waBinary.Node, 0, len(children)+1)
	var biz *waBinary.Node
	for i := range children {
		if biz == nil && children[i].Tag == "biz" {
			b := children[i]
			biz = &b
			continue
		}
		newContent = append(newContent, children[i])
	}
	if biz == nil {
		// No <biz> node was built (shouldn't happen for interactive types); leave
		// the stanza untouched rather than emit a lone <bot>.
		return
	}
	newContent = append(newContent, *biz)
	var bot *waBinary.Node
	if !isCarouselOrCatalog(msg) {
		bot = &waBinary.Node{Tag: "bot", Attrs: waBinary.Attrs{"biz_bot": "1"}}
		newContent = append(newContent, *bot)
	}
	node.Content = newContent

	// Audit log of the cleartext interactive routing nodes that were emitted.
	if cli != nil && cli.Log != nil {
		botStr := "<none>"
		if bot != nil {
			botStr = bot.String()
		}
		cli.Log.Infof("Interactive stanza nodes for %s: biz=%s bot=%s", to, biz.String(), botStr)
	}
}

// preferLIDForCarousel returns the LID the envelope should be addressed to for a
// carousel sent to a phone-number JID, replicating the reference: prefer the LID
// when a mapping exists, otherwise fall back (the caller keeps the normalized PN
// — it never addresses to an empty JID). Returns EmptyJID when no LID is known.
//
// This addressing is needed for carousels to avoid a server error 400.
func (cli *Client) preferLIDForCarousel(ctx context.Context, to types.JID) types.JID {
	if cli == nil || to.Server != types.DefaultUserServer {
		return types.EmptyJID
	}
	lid, err := cli.Store.LIDs.GetLIDForPN(ctx, to)
	if err != nil {
		cli.Log.Warnf("Carousel: failed to get LID for %s: %v (falling back to PN)", to, err)
		return types.EmptyJID
	}
	if lid.IsEmpty() {
		// Try a server lookup to populate the mapping; ignore failures (fall back).
		if info, err := cli.GetUserInfo(ctx, []types.JID{to}); err == nil {
			lid = info[to].LID
		}
	}
	return lid
}
