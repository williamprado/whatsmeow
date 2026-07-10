// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Command voip-receive is the Phase 0 manual test for voice-call RECEIVING.
// It connects an already-logged-in session and, only when VOIP_ENABLED=1,
// listens for incoming calls, logs them (caller, callID, timestamp) and
// auto-rejects them cleanly. NO audio is performed.
//
// ⚠️ EXPERIMENTAL — HIGH ACCOUNT-BAN RISK. VoIP via an unofficial library is far
// more sensitive than messages. Use ONLY a disposable test account (the one
// already paired in examples/interactive-test/session.db). NEVER the production
// (atendzappy) account. To trigger a test, place a WhatsApp call TO the disposable
// account from another phone; this program will detect and reject it.
//
// Usage:
//
//	VOIP_ENABLED=1 go run ./examples/voip-receive   # listen + auto-reject
//	go run ./examples/voip-receive                  # VoIP off (default) — does nothing
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	_ "modernc.org/sqlite"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	"go.mau.fi/whatsmeow/voip"
)

// Reuse the session already paired by the interactive-test example.
const sessionFile = "examples/interactive-test/session.db"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	enabled := os.Getenv("VOIP_ENABLED") == "1"
	fmt.Println("⚠️  EXPERIMENTAL VoIP (Phase 0, receive-only, no audio). DISPOSABLE account only — never production.")

	ctx := context.Background()
	dbLog := waLog.Stdout("Database", "INFO", true)
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
	if device.ID == nil {
		return fmt.Errorf("no logged-in session in %s — pair a disposable account first via examples/interactive-test", sessionFile)
	}

	clientLog := waLog.Stdout("Client", "INFO", true)
	client := whatsmeow.NewClient(device, clientLog)

	mgr := voip.NewManager(client, voip.Config{Enabled: enabled}, waLog.Stdout("VoIP", "INFO", true))
	mgr.OnIncomingCall(func(c voip.IncomingCall) {
		fmt.Printf("📞 Incoming call: id=%s from=%s creator=%s ts=%s\n", c.CallID, c.From, c.CallCreator, c.Timestamp)
		// Phase 0: auto-reject cleanly (no audio).
		if err := mgr.Reject(ctx, c); err != nil {
			fmt.Printf("❌ reject failed: %v\n", err)
		} else {
			fmt.Printf("✅ rejected call id=%s\n", c.CallID)
		}
	})

	if err = client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	fmt.Println("✅ Connected (reused session).")
	mgr.Start() // no-op when VOIP_ENABLED!=1

	if enabled {
		fmt.Println("Listening for incoming calls. Place a WhatsApp call TO this account from another phone. Ctrl+C to exit.")
	} else {
		fmt.Println("VoIP disabled (set VOIP_ENABLED=1 to listen). Ctrl+C to exit.")
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	mgr.Stop()
	client.Disconnect()
	return nil
}
