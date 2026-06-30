// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// ============================================================================
// CUSTOM FORK PATCH — native-flow list / quick-reply (renderable formats)
// ============================================================================
//
// Field testing (PR #2) showed the legacy ButtonsMessage / ListMessage are
// dropped by the recipient, while native-flow InteractiveMessage renders. The
// reference implementation (rsalcara/InfiniteAPI, src/Utils/messages.ts
// generateListMessage) does NOT use the legacy types: its "list" is an
// InteractiveMessage carrying a single native-flow button named "single_select"
// whose buttonParamsJson holds the sections/rows, and the InteractiveMessage is
// wrapped in a ViewOnceMessage (for iOS/Android compatibility).
//
// BuildNativeFlowListMessage reproduces that exact shape. The <biz>/<bot>
// routing nodes and native_flow v="9" are added automatically by the bot-node
// patch (send_interactive_patch.go), since this is an InteractiveMessage.
//
// ⚠️ EXPERIMENTAL — account-ban risk. Disposable accounts only.

// nativeFlowSingleSelect is the native-flow button name WhatsApp uses to render
// a single-select list.
const nativeFlowSingleSelect = "single_select"

// List size limits enforced by WhatsApp (mirrors LIST_LIMITS in the reference).
const (
	MaxListSections   = 10
	MaxListRowsPerSec = 10
	MaxListRowsTotal  = 100
)

// nativeFlowListRow / nativeFlowListSection / nativeFlowListParams are the JSON
// shape of the single_select buttonParamsJson:
//
//	{"title":"<buttonText>","sections":[{"title":"<sec>","rows":[{"id":"..","title":"..","description":".."}]}]}
type nativeFlowListRow struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type nativeFlowListSection struct {
	Title string              `json:"title"`
	Rows  []nativeFlowListRow `json:"rows"`
}

type nativeFlowListParams struct {
	Title    string                  `json:"title"`
	Sections []nativeFlowListSection `json:"sections"`
}

// BuildNativeFlowListMessage builds a single-select list as a native-flow
// InteractiveMessage wrapped in a ViewOnceMessage — the renderable format used
// by the reference implementation (instead of the legacy, dropped ListMessage).
//
//   - buttonText: label of the button that opens the list (e.g. "Open menu").
//   - body:       the message text shown above the button.
//   - footer:     optional small print (pass "" to omit).
//   - header:     optional header (use NewInteractiveHeaderText/.../nil).
//   - sections:   the selectable sections/rows (reuses ListSection/ListRow; the
//     row RowID becomes the "id" returned in the response).
//
// It validates the WhatsApp list limits (MaxListSections, MaxListRowsPerSec,
// MaxListRowsTotal) and returns an error if exceeded.
func BuildNativeFlowListMessage(buttonText, body, footer string, header *waE2E.InteractiveMessage_Header, sections []ListSection) (*waE2E.Message, error) {
	if len(sections) == 0 {
		return nil, fmt.Errorf("native flow list: at least one section is required")
	}
	if len(sections) > MaxListSections {
		return nil, fmt.Errorf("native flow list: %d sections exceeds the max of %d", len(sections), MaxListSections)
	}
	total := 0
	jsonSections := make([]nativeFlowListSection, len(sections))
	for i, sec := range sections {
		if len(sec.Rows) == 0 {
			return nil, fmt.Errorf("native flow list: section %q has no rows", sec.Title)
		}
		if len(sec.Rows) > MaxListRowsPerSec {
			return nil, fmt.Errorf("native flow list: section %q has %d rows, exceeds the max of %d", sec.Title, len(sec.Rows), MaxListRowsPerSec)
		}
		total += len(sec.Rows)
		jsonRows := make([]nativeFlowListRow, len(sec.Rows))
		for j, row := range sec.Rows {
			jsonRows[j] = nativeFlowListRow{
				ID:          row.RowID,
				Title:       row.Title,
				Description: row.Description, // "" is fine; matches the reference
			}
		}
		jsonSections[i] = nativeFlowListSection{Title: sec.Title, Rows: jsonRows}
	}
	if total > MaxListRowsTotal {
		return nil, fmt.Errorf("native flow list: %d total rows exceeds the max of %d", total, MaxListRowsTotal)
	}

	params := nativeFlowListParams{Title: buttonText, Sections: jsonSections}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("native flow list: marshal params: %w", err)
	}

	nf := &waE2E.InteractiveMessage_NativeFlowMessage{
		Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{{
			Name:             proto.String(nativeFlowSingleSelect),
			ButtonParamsJSON: proto.String(string(rawParams)),
		}},
		MessageParamsJSON: proto.String("{}"),
		MessageVersion:    proto.Int32(2),
	}
	im := &waE2E.InteractiveMessage{
		Header: header,
		Body:   &waE2E.InteractiveMessage_Body{Text: proto.String(body)},
		InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
			NativeFlowMessage: nf,
		},
	}
	if footer != "" {
		im.Footer = &waE2E.InteractiveMessage_Footer{Text: proto.String(footer)}
	}

	// Wrap in ViewOnceMessage, matching the reference (iOS/Android compatibility).
	return &waE2E.Message{
		ViewOnceMessage: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{InteractiveMessage: im},
		},
	}, nil
}

// BuildNativeFlowQuickReplyMessage builds quick-reply buttons as a native-flow
// InteractiveMessage — the renderable replacement for the deprecated,
// recipient-dropped BuildButtonsMessage. Each button replies with its id.
//
// Deprecated path note: prefer this over BuildButtonsMessage, which produces a
// legacy ButtonsMessage that recipients drop.
func BuildNativeFlowQuickReplyMessage(body, footer string, header *waE2E.InteractiveMessage_Header, buttons []QuickReplyButton) *waE2E.Message {
	nfButtons := make([]*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton, len(buttons))
	for i, b := range buttons {
		nfButtons[i] = NewQuickReplyNativeFlowButton(b.DisplayText, b.ID)
	}
	return BuildInteractiveMessage(body, footer, header, nfButtons)
}
