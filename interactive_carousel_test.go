// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

func twoValidCards() []CarouselCard {
	return []CarouselCard{
		{Title: "Card 1", Body: "b1", Footer: "f1", Image: &waE2E.ImageMessage{}, Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
			NewURLNativeFlowButton("Buy", "https://example.com/1"),
		}},
		{Title: "Card 2", Body: "b2", Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
			NewQuickReplyNativeFlowButton("Pick", "pick-2"),
		}},
	}
}

func TestBuildCarouselMessageStructure(t *testing.T) {
	msg, err := BuildCarouselMessage("Top title", "Top body", "Top footer", twoValidCards())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// NOT wrapped in ViewOnce — carousel is the root InteractiveMessage.
	if msg.GetViewOnceMessage() != nil {
		t.Error("carousel must NOT be wrapped in ViewOnce")
	}
	im := msg.GetInteractiveMessage()
	if im == nil {
		t.Fatal("InteractiveMessage is nil")
	}
	if im.GetHeader().GetTitle() != "Top title" || im.GetHeader().GetHasMediaAttachment() {
		t.Errorf("top header = %+v, want title 'Top title' hasMediaAttachment=false", im.GetHeader())
	}
	if im.GetBody().GetText() != "Top body" || im.GetFooter().GetText() != "Top footer" {
		t.Errorf("top body/footer wrong: %q / %q", im.GetBody().GetText(), im.GetFooter().GetText())
	}

	c := im.GetCarouselMessage()
	if c == nil {
		t.Fatal("CarouselMessage is nil")
	}
	if c.GetMessageVersion() != 1 {
		t.Errorf("messageVersion = %d, want 1", c.GetMessageVersion())
	}
	cards := c.GetCards()
	if len(cards) != 2 {
		t.Fatalf("got %d cards, want 2", len(cards))
	}

	// Card 1: title, body, footer, subtitle(=footer), media + hasMediaAttachment.
	c1 := cards[0]
	if c1.GetHeader().GetTitle() != "Card 1" || c1.GetHeader().GetSubtitle() != "f1" {
		t.Errorf("card1 header = %+v, want title 'Card 1' subtitle 'f1'", c1.GetHeader())
	}
	if !c1.GetHeader().GetHasMediaAttachment() || c1.GetHeader().GetImageMessage() == nil {
		t.Error("card1 should have an image and hasMediaAttachment=true")
	}
	if c1.GetBody().GetText() != "b1" || c1.GetFooter().GetText() != "f1" {
		t.Errorf("card1 body/footer wrong: %q / %q", c1.GetBody().GetText(), c1.GetFooter().GetText())
	}
	if got := c1.GetNativeFlowMessage().GetButtons()[0].GetName(); got != nativeFlowCTAURL {
		t.Errorf("card1 button name = %q, want cta_url", got)
	}

	// Card 2: no media, no footer.
	c2 := cards[1]
	if c2.GetHeader().GetHasMediaAttachment() || c2.GetHeader().GetImageMessage() != nil {
		t.Error("card2 should have no media")
	}
	if c2.GetFooter() != nil {
		t.Error("card2 footer should be nil")
	}
}

func TestBuildCarouselValidation(t *testing.T) {
	// Too few cards.
	if _, err := BuildCarouselMessage("t", "b", "", twoValidCards()[:1]); err == nil {
		t.Error("expected error for <2 cards")
	}
	// Too many cards.
	many := make([]CarouselCard, MaxCarouselCards+1)
	for i := range many {
		many[i] = CarouselCard{Title: "c", Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{NewQuickReplyNativeFlowButton("x", "y")}}
	}
	if _, err := BuildCarouselMessage("t", "b", "", many); err == nil {
		t.Error("expected error for >10 cards")
	}
	// Card with no buttons.
	noBtn := twoValidCards()
	noBtn[1].Buttons = nil
	if _, err := BuildCarouselMessage("t", "b", "", noBtn); err == nil {
		t.Error("expected error for a card with no buttons")
	}
	// Card with both image and video.
	both := twoValidCards()
	both[0].Video = &waE2E.VideoMessage{}
	if _, err := BuildCarouselMessage("t", "b", "", both); err == nil {
		t.Error("expected error for a card with both image and video")
	}
}

func TestCarouselBizNodeHasQualityControl(t *testing.T) {
	msg, _ := BuildCarouselMessage("t", "b", "", twoValidCards())
	node, ok := customInteractiveBizNode(msg)
	if !ok {
		t.Fatal("customInteractiveBizNode returned ok=false for carousel")
	}
	children := node.GetChildren()
	var hasInteractive bool
	var decisionID, sourceValue string
	for _, ch := range children {
		switch ch.Tag {
		case "interactive":
			hasInteractive = true
		case "quality_control":
			decisionID, _ = ch.Attrs["decision_id"].(string)
			if src := ch.GetChildren(); len(src) == 1 && src[0].Tag == "decision_source" {
				sourceValue, _ = src[0].Attrs["value"].(string)
			}
		}
	}
	if !hasInteractive {
		t.Fatalf("biz children = %v, want an interactive node", children)
	}
	if len(decisionID) != 40 { // 20 random bytes hex-encoded
		t.Errorf("decision_id = %q (len %d), want 40 hex chars", decisionID, len(decisionID))
	}
	if sourceValue != "df" {
		t.Errorf("decision_source value = %q, want df", sourceValue)
	}

	// Carousel must be excluded from the bot node.
	if !isCarouselOrCatalog(msg) {
		t.Error("isCarouselOrCatalog should be true for carousel (so the bot node is omitted)")
	}
}

func TestNonCarouselBizNodeHasNoQualityControl(t *testing.T) {
	msg := BuildInteractiveMessage("b", "", nil, []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
		NewQuickReplyNativeFlowButton("Yes", "y"),
	})
	node, _ := customInteractiveBizNode(msg)
	for _, ch := range node.GetChildren() {
		if ch.Tag == "quality_control" {
			t.Error("non-carousel biz node must not have quality_control")
		}
	}
}

func TestCarouselTopMediaHeader(t *testing.T) {
	// Without top media: header has no media, hasMediaAttachment=false.
	plain, err := BuildCarouselMessage("t", "b", "", twoValidCards())
	if err != nil {
		t.Fatal(err)
	}
	ph := plain.GetInteractiveMessage().GetHeader()
	if ph.GetHasMediaAttachment() || ph.GetImageMessage() != nil || ph.GetVideoMessage() != nil {
		t.Error("plain carousel top header should have no media")
	}

	// With top image media: header carries it, hasMediaAttachment=true.
	withImg, err := BuildCarouselMessageWithOptions(CarouselOptions{
		Title: "t", Body: "b", HeaderImage: &waE2E.ImageMessage{}, Cards: twoValidCards(),
	})
	if err != nil {
		t.Fatal(err)
	}
	h := withImg.GetInteractiveMessage().GetHeader()
	if !h.GetHasMediaAttachment() || h.GetImageMessage() == nil {
		t.Error("top header should carry the image with hasMediaAttachment=true")
	}

	// With top video media.
	withVid, err := BuildCarouselMessageWithOptions(CarouselOptions{
		Title: "t", Body: "b", HeaderVideo: &waE2E.VideoMessage{}, Cards: twoValidCards(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if withVid.GetInteractiveMessage().GetHeader().GetVideoMessage() == nil {
		t.Error("top header should carry the video")
	}
}

func TestCarouselTopHeaderMediaExclusive(t *testing.T) {
	_, err := BuildCarouselMessageWithOptions(CarouselOptions{
		Title: "t", Body: "b", HeaderImage: &waE2E.ImageMessage{}, HeaderVideo: &waE2E.VideoMessage{},
		Cards: twoValidCards(),
	})
	if err == nil {
		t.Error("expected error when top header has both image and video")
	}
}

func TestUploadCarouselVideoValidation(t *testing.T) {
	// These validations run before cli.Upload, so a nil client is fine.
	if _, err := (*Client)(nil).UploadCarouselVideo(context.Background(), CarouselVideo{}); err == nil {
		t.Error("expected error for empty video data")
	}
	if _, err := (*Client)(nil).UploadCarouselVideo(context.Background(), CarouselVideo{Data: []byte("x")}); err == nil {
		t.Error("expected error for missing thumbnail")
	}
}

func TestJPEGThumbnail(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 600, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 600; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x80, A: 255})
		}
	}
	thumb := jpegThumbnail(src, carouselThumbnailMaxDim)
	if len(thumb) == 0 {
		t.Fatal("thumbnail is empty")
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(thumb))
	if err != nil {
		t.Fatalf("thumbnail is not valid JPEG: %v", err)
	}
	// Downscaled so the longest side fits maxDim (landscape -> width capped).
	if cfg.Width != carouselThumbnailMaxDim || cfg.Width > 600 || cfg.Height > 400 {
		t.Errorf("thumbnail size = %dx%d, want width %d", cfg.Width, cfg.Height, carouselThumbnailMaxDim)
	}
}

func TestCopyNativeFlowButton(t *testing.T) {
	btn := NewCopyNativeFlowButton("Copy code", "ABC123")
	if btn.GetName() != nativeFlowCTACopy {
		t.Errorf("name = %q, want cta_copy", btn.GetName())
	}
	var p map[string]string
	if err := json.Unmarshal([]byte(btn.GetButtonParamsJSON()), &p); err != nil {
		t.Fatalf("params not JSON: %v", err)
	}
	if p["display_text"] != "Copy code" || p["copy_code"] != "ABC123" {
		t.Errorf("params = %v, want display_text/copy_code", p)
	}
}
