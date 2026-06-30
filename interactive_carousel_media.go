// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // register PNG decoder

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// ============================================================================
// CUSTOM FORK PATCH — carousel media upload helpers
// ============================================================================
//
// The reference (rsalcara/InfiniteAPI) ensures every image carousel card has a
// jpegThumbnail and width/height, and that video cards carry a thumbnail and
// dimensions — these are required for the card to render on WhatsApp Web. These
// helpers upload media via the native cli.Upload flow and build the
// ImageMessage / VideoMessage with those fields filled in.
//
// ⚠️ EXPERIMENTAL — account-ban risk. Disposable accounts only.

// carouselThumbnailMaxDim is the max width/height of the generated JPEG thumbnail.
const carouselThumbnailMaxDim = 240

// UploadCarouselImage uploads image data (PNG or JPEG) and builds an
// *waE2E.ImageMessage ready for a carousel card or the top media header. The
// width/height are decoded from the image and a JPEG thumbnail is generated —
// both required for the card to render on Web.
func (cli *Client) UploadCarouselImage(ctx context.Context, data []byte) (*waE2E.ImageMessage, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("carousel image: decode config: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("carousel image: decode: %w", err)
	}
	up, err := cli.Upload(ctx, data, MediaImage)
	if err != nil {
		return nil, fmt.Errorf("carousel image: upload: %w", err)
	}
	return &waE2E.ImageMessage{
		URL:           proto.String(up.URL),
		DirectPath:    proto.String(up.DirectPath),
		MediaKey:      up.MediaKey,
		FileEncSHA256: up.FileEncSHA256,
		FileSHA256:    up.FileSHA256,
		FileLength:    proto.Uint64(up.FileLength),
		Mimetype:      proto.String("image/" + format),
		Width:         proto.Uint32(uint32(cfg.Width)),
		Height:        proto.Uint32(uint32(cfg.Height)),
		JPEGThumbnail: jpegThumbnail(img, carouselThumbnailMaxDim),
	}, nil
}

// CarouselVideo holds the already-prepared inputs for UploadCarouselVideo. Go
// cannot extract a frame/dimensions from an arbitrary video without an external
// tool (the reference uses ffmpeg), so the caller supplies the JPEG thumbnail
// and dimensions/duration. See examples/interactive-test for an ffmpeg-based
// way to produce them.
type CarouselVideo struct {
	// Data is the raw video bytes (e.g. an MP4).
	Data []byte
	// JPEGThumbnail is a poster frame as JPEG. Required for the card to render.
	JPEGThumbnail []byte
	Width         uint32
	Height        uint32
	Seconds       uint32
	// Mimetype defaults to "video/mp4" when empty.
	Mimetype string
	// GifPlayback marks the video as a GIF-style autoplay clip.
	GifPlayback bool
}

// UploadCarouselVideo uploads video data and builds an *waE2E.VideoMessage with
// the supplied thumbnail and dimensions/duration, ready for a carousel card or
// the top media header.
func (cli *Client) UploadCarouselVideo(ctx context.Context, vid CarouselVideo) (*waE2E.VideoMessage, error) {
	if len(vid.Data) == 0 {
		return nil, fmt.Errorf("carousel video: empty data")
	}
	if len(vid.JPEGThumbnail) == 0 {
		return nil, fmt.Errorf("carousel video: a JPEG thumbnail is required for the card to render")
	}
	up, err := cli.Upload(ctx, vid.Data, MediaVideo)
	if err != nil {
		return nil, fmt.Errorf("carousel video: upload: %w", err)
	}
	mimetype := vid.Mimetype
	if mimetype == "" {
		mimetype = "video/mp4"
	}
	return &waE2E.VideoMessage{
		URL:           proto.String(up.URL),
		DirectPath:    proto.String(up.DirectPath),
		MediaKey:      up.MediaKey,
		FileEncSHA256: up.FileEncSHA256,
		FileSHA256:    up.FileSHA256,
		FileLength:    proto.Uint64(up.FileLength),
		Mimetype:      proto.String(mimetype),
		Width:         proto.Uint32(vid.Width),
		Height:        proto.Uint32(vid.Height),
		Seconds:       proto.Uint32(vid.Seconds),
		GifPlayback:   proto.Bool(vid.GifPlayback),
		JPEGThumbnail: vid.JPEGThumbnail,
	}, nil
}

// jpegThumbnail downscales src (nearest-neighbor, no external deps) to fit
// maxDim and encodes it as JPEG.
func jpegThumbnail(src image.Image, maxDim int) []byte {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil
	}
	nw, nh := w, h
	switch {
	case w >= h && w > maxDim:
		nw, nh = maxDim, h*maxDim/w
	case h > w && h > maxDim:
		nw, nh = w*maxDim/h, maxDim
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*w/nw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 70}); err != nil {
		return nil
	}
	return buf.Bytes()
}
