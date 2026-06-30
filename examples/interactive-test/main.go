// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Command interactive-test is a manual smoke test for the interactive (button)
// message helpers. It logs in via QR code (printed as ASCII in the terminal) and
// sends a few button messages to a configurable recipient.
//
// ⚠️ EXPERIMENTAL — ACCOUNT-BAN RISK
//
// Sending interactive messages from non-official libraries is experimental and
// WhatsApp may BAN the sending account. Scan the QR code ONLY with a disposable
// test account. NEVER use the production (atendzappy) account.
//
// Usage:
//
//	export TEST_RECIPIENT=5577988272902      # recipient phone number, digits only (no +)
//	go run ./examples/interactive-test
//
// The session is stored in examples/interactive-test/session.db (gitignored), so
// the QR code is only needed on the first run.
//
// Note: with the current send core, only the quick-reply buttons and list
// message render on the recipient's device. The template, native-flow and
// carousel sends are included to exercise the helpers, but they will NOT render
// until the send.go core patch is applied (see docs/interactive_messages.md).
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mdp/qrterminal/v3"
	qrcode "github.com/skip2/go-qrcode"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

const sessionFile = "examples/interactive-test/session.db"

// qrImageFile is overwritten with a scannable PNG on every QR refresh, so a
// terminal that can't render the ASCII QR can open the image instead.
const qrImageFile = "examples/interactive-test/qr.png"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	rawRecipient := os.Getenv("TEST_RECIPIENT")
	if rawRecipient == "" {
		return errors.New("set TEST_RECIPIENT to the destination phone number (digits only, e.g. 5577988272902); comma-separate to check several")
	}
	// TEST_RECIPIENT may be a comma-separated list (useful with CHECK_ONLY to
	// compare e.g. the number with and without the Brazilian 9th digit).
	var recipients []string
	for _, r := range strings.Split(rawRecipient, ",") {
		if r = strings.TrimSpace(r); r != "" {
			recipients = append(recipients, r)
		}
	}
	to := types.NewJID(recipients[0], types.DefaultUserServer)

	fmt.Println("⚠️  EXPERIMENTAL: scan the QR ONLY with a disposable test account. Do NOT use the production account.")

	ctx := context.Background()
	dbLog := waLog.Stdout("Database", "INFO", true)

	// Open SQLite with the pure-Go modernc driver (registered as "sqlite") so no
	// C compiler is required, but tell whatsmeow the dialect is "sqlite3".
	// modernc uses the `_pragma=` DSN syntax (not mattn's `_foreign_keys=on`);
	// whatsmeow's Upgrade refuses to run unless foreign keys are enabled.
	db, err := sql.Open("sqlite", "file:"+sessionFile+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(15000)")
	if err != nil {
		return fmt.Errorf("open session db: %w", err)
	}
	defer db.Close()
	container := sqlstore.NewWithDB(db, "sqlite3", dbLog)
	if err = container.Upgrade(ctx); err != nil {
		return fmt.Errorf("upgrade session db: %w", err)
	}

	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("get device: %w", err)
	}

	clientLog := waLog.Stdout("Client", "INFO", true)
	client := whatsmeow.NewClient(device, clientLog)

	// Log any button/list/template/native-flow responses the recipient sends
	// back, so a tapped button shows up here with its payload.
	client.AddEventHandler(logButtonResponses)

	loggedIn := make(chan struct{}, 1)

	if client.Store.ID == nil {
		// No session yet: log in via QR.
		qrChan, err := client.GetQRChannel(ctx)
		if err != nil {
			return fmt.Errorf("get QR channel: %w", err)
		}
		if err = client.Connect(); err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		go func() {
			for item := range qrChan {
				switch item.Event {
				case whatsmeow.QRChannelEventCode:
					// ASCII QR in the terminal is the default. Set QR_PNG=1 to
					// also write a PNG (qr.png) to scan from an image viewer when
					// the terminal can't render the ASCII blocks.
					fmt.Println("\nScan this QR code with your test WhatsApp account:")
					qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, os.Stdout)
					if os.Getenv("QR_PNG") != "" {
						if err := qrcode.WriteFile(item.Code, qrcode.Medium, 512, qrImageFile); err != nil {
							fmt.Printf("(failed to write QR image: %v)\n", err)
						} else {
							fmt.Printf("QR image (open/scan this): %s\n", qrImageFile)
						}
					}
				case "success":
					fmt.Println("\n✅ Logged in successfully.")
					loggedIn <- struct{}{}
				default:
					fmt.Printf("QR channel event: %s (err=%v)\n", item.Event, item.Error)
				}
			}
		}()
		select {
		case <-loggedIn:
		case <-time.After(3 * time.Minute):
			return errors.New("timed out waiting for QR login")
		}
	} else {
		// Existing session: just connect.
		if err = client.Connect(); err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		fmt.Println("✅ Reused existing session.")
	}

	// Give the connection a moment to settle before sending. On a warm session
	// this lets the post-connect app-state/contact sync finish so it doesn't
	// contend with message encryption on the single SQLite connection (which on
	// the first login produced SQLITE_BUSY and downstream server errors).
	time.Sleep(8 * time.Second)

	// Resolve the recipient number(s) on WhatsApp. This shows whether the number
	// is registered and what its canonical JID is — useful to diagnose the
	// Brazilian 9th-digit issue (a message "sent" to an unregistered JID is
	// accepted by the server but delivered to nobody).
	if onWA, err := client.IsOnWhatsApp(ctx, recipients); err != nil {
		fmt.Printf("IsOnWhatsApp query failed: %v\n", err)
	} else {
		for _, r := range onWA {
			fmt.Printf("IsOnWhatsApp: query=%s registered=%v jid=%s\n", r.Query, r.IsIn, r.JID)
			// Send to the canonical JID the server reports for our first
			// recipient. This handles the Brazilian 9th-digit case, where the
			// real account JID differs from the dialed number.
			if r.IsIn && !r.JID.IsEmpty() && r.Query == recipients[0] {
				if r.JID != to {
					fmt.Printf("Using canonical JID %s (was %s)\n", r.JID, to)
				}
				to = r.JID
			}
		}
	}

	if os.Getenv("CHECK_ONLY") != "" {
		fmt.Println("CHECK_ONLY set — not sending any messages.")
		client.Disconnect()
		return nil
	}

	if os.Getenv("LISTEN_ONLY") != "" {
		fmt.Println("LISTEN_ONLY set — not sending; listening for button responses. Press Ctrl+C to exit.")
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop
		client.Disconnect()
		return nil
	}

	sendAll(ctx, client, to)

	fmt.Println("\nDone. Press Ctrl+C to exit (or it will exit after disconnect).")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	client.Disconnect()
	return nil
}

// logButtonResponses prints any interactive response the recipient sends back
// (a tapped quick-reply, list row, template button or native-flow button),
// including the selected ID and payload.
func logButtonResponses(evt any) {
	m, ok := evt.(*events.Message)
	if !ok || m.Message == nil {
		return
	}
	msg := m.Message
	switch {
	case msg.GetButtonsResponseMessage() != nil:
		r := msg.GetButtonsResponseMessage()
		fmt.Printf("\n↩️  buttonsResponse from %s: selectedButtonID=%q displayText=%q\n",
			m.Info.Sender, r.GetSelectedButtonID(), r.GetSelectedDisplayText())
	case msg.GetListResponseMessage() != nil:
		r := msg.GetListResponseMessage()
		fmt.Printf("\n↩️  listResponse from %s: selectedRowID=%q title=%q\n",
			m.Info.Sender, r.GetSingleSelectReply().GetSelectedRowID(), r.GetTitle())
	case msg.GetTemplateButtonReplyMessage() != nil:
		r := msg.GetTemplateButtonReplyMessage()
		fmt.Printf("\n↩️  templateButtonReply from %s: selectedID=%q displayText=%q index=%d\n",
			m.Info.Sender, r.GetSelectedID(), r.GetSelectedDisplayText(), r.GetSelectedIndex())
	case msg.GetInteractiveResponseMessage() != nil:
		r := msg.GetInteractiveResponseMessage().GetNativeFlowResponseMessage()
		fmt.Printf("\n↩️  interactiveResponse(nativeFlow) from %s: name=%q paramsJSON=%s\n",
			m.Info.Sender, r.GetName(), r.GetParamsJSON())
	}
}

// testMessage pairs a human label with a message and whether it is expected to
// render with the current (unpatched) send core.
type testMessage struct {
	label        string
	msg          *waE2E.Message
	rendersToday bool
}

func sendAll(ctx context.Context, client *whatsmeow.Client, to types.JID) {
	messages := []testMessage{
		{
			// Control: a plain text message. If this arrives but the button
			// messages don't, the buttons are being dropped. If even this
			// doesn't arrive, the problem is delivery/account, not the buttons.
			label:        "ControlText (plain text)",
			rendersToday: true,
			msg:          &waE2E.Message{Conversation: proto.String("Control: plain text from interactive-test")},
		},
		{
			label:        "ButtonsMessage (3 quick replies)",
			rendersToday: true,
			msg: whatsmeow.BuildButtonsMessage("Smoke test: quick replies", "interactive-test", []whatsmeow.QuickReplyButton{
				{ID: "opt-1", DisplayText: "Option 1"},
				{ID: "opt-2", DisplayText: "Option 2"},
				{ID: "opt-3", DisplayText: "Option 3"},
			}),
		},
		{
			label:        "ListMessage (2 sections)",
			rendersToday: true,
			msg: whatsmeow.BuildListMessage("Smoke test list", "Pick something", "Open list", "interactive-test", []whatsmeow.ListSection{
				{
					Title: "Section A",
					Rows: []whatsmeow.ListRow{
						{Title: "A1", Description: "first", RowID: "a1"},
						{Title: "A2", RowID: "a2"},
					},
				},
				{
					Title: "Section B",
					Rows: []whatsmeow.ListRow{
						{Title: "B1", RowID: "b1"},
					},
				},
			}),
		},
		{
			label:        "TemplateMessage (reply/url/call)",
			rendersToday: false,
			msg: whatsmeow.BuildTemplateMessage("Smoke test: template buttons", "interactive-test", []*waE2E.HydratedTemplateButton{
				whatsmeow.NewQuickReplyTemplateButton("Reply", "tmpl-reply"),
				whatsmeow.NewURLTemplateButton("Open", "https://example.com"),
				whatsmeow.NewCallTemplateButton("Call", "+5511999999999"),
			}),
		},
		{
			label:        "InteractiveMessage (native flow)",
			rendersToday: false,
			msg: whatsmeow.BuildInteractiveMessage("Smoke test: native flow", "interactive-test",
				whatsmeow.NewInteractiveHeaderText("Header", "Subtitle"),
				[]*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
					whatsmeow.NewQuickReplyNativeFlowButton("Yes", "nf-yes"),
					whatsmeow.NewURLNativeFlowButton("Site", "https://example.com"),
				}),
		},
		{
			label:        "CarouselMessage (2 cards)",
			rendersToday: false,
			msg: whatsmeow.BuildCarouselMessage("Smoke test: carousel", []whatsmeow.CarouselCard{
				{
					Body:   "Card 1",
					Footer: "card one",
					Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
						whatsmeow.NewURLNativeFlowButton("Buy 1", "https://example.com/1"),
					},
				},
				{
					Body: "Card 2",
					Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
						whatsmeow.NewQuickReplyNativeFlowButton("Pick 2", "card-2"),
					},
				},
			}),
		},
	}

	for _, tm := range messages {
		note := ""
		if !tm.rendersToday {
			note = "  (likely WON'T render until the send.go core patch — see docs/interactive_messages.md)"
		}
		resp, err := client.SendMessage(ctx, to, tm.msg)
		if err != nil {
			fmt.Printf("❌ %s: %v%s\n", tm.label, err, note)
		} else {
			fmt.Printf("✅ %s: sent (id=%s)%s\n", tm.label, resp.ID, note)
		}
		// Small gap between sends to keep ordering and avoid rate spikes.
		time.Sleep(1500 * time.Millisecond)
	}
}
