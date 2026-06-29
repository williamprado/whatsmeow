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
	"syscall"
	"time"

	"github.com/mdp/qrterminal/v3"
	_ "modernc.org/sqlite"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

const sessionFile = "examples/interactive-test/session.db"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	recipient := os.Getenv("TEST_RECIPIENT")
	if recipient == "" {
		return errors.New("set TEST_RECIPIENT to the destination phone number (digits only, e.g. 5577988272902)")
	}
	to := types.NewJID(recipient, types.DefaultUserServer)

	fmt.Println("⚠️  EXPERIMENTAL: scan the QR ONLY with a disposable test account. Do NOT use the production account.")

	ctx := context.Background()
	dbLog := waLog.Stdout("Database", "INFO", true)

	// Open SQLite with the pure-Go modernc driver (registered as "sqlite") so no
	// C compiler is required, but tell whatsmeow the dialect is "sqlite3".
	db, err := sql.Open("sqlite", "file:"+sessionFile+"?_foreign_keys=on")
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
					fmt.Println("\nScan this QR code with your test WhatsApp account:")
					qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, os.Stdout)
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

	// Give the connection a moment to settle before sending.
	time.Sleep(2 * time.Second)

	sendAll(ctx, client, to)

	fmt.Println("\nDone. Press Ctrl+C to exit (or it will exit after disconnect).")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	client.Disconnect()
	return nil
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
