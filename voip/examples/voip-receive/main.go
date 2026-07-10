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
	"time"

	_ "modernc.org/sqlite"

	"github.com/williamprado/whatsmeow/voip"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Reuse the session already paired by the interactive-test example. The default
// path is relative to the voip module dir; override with SESSION_DB.
func sessionFile() string {
	if p := os.Getenv("SESSION_DB"); p != "" {
		return p
	}
	return "../../examples/interactive-test/session.db"
}

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
	db, err := sql.Open("sqlite", "file:"+sessionFile()+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(15000)")
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
		return fmt.Errorf("no logged-in session in %s — pair a disposable account first via examples/interactive-test", sessionFile())
	}

	clientLog := waLog.Stdout("Client", "INFO", true)
	client := whatsmeow.NewClient(device, clientLog)

	mgr := voip.NewManager(client, voip.Config{Enabled: enabled}, waLog.Stdout("VoIP", "INFO", true))
	mgr.OnIncomingCall(func(c voip.IncomingCall) {
		fmt.Printf("📞 Incoming call: id=%s from=%s creator=%s ts=%s\n", c.CallID, c.From, c.CallCreator, c.Timestamp)
		// Verify the call-key crypto on real data: decrypt the offer's call key.
		if key, err := mgr.DecryptCallKey(ctx, c); err != nil {
			fmt.Printf("⚠️  decrypt call key failed: %v\n", err)
		} else {
			fmt.Printf("🔑 decrypted call key: %d bytes (prefix %x)\n", len(key), key[:min(4, len(key))])
		}
		// No audio in this phase: reject cleanly.
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

	// Optional: initiate an outgoing call (offer handshake only, no audio) when
	// VOIP_CALL=<number> is set. Use a 2nd DISPOSABLE account's number.
	if enabled {
		if number := os.Getenv("VOIP_CALL"); number != "" {
			go startCall(ctx, client, mgr, number)
		}
	}

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

// startCall initiates an outgoing call's offer handshake (no audio) to a number,
// logs the relay endpoints from the ack, then terminates. Disposable accounts only.
func startCall(ctx context.Context, client *whatsmeow.Client, mgr *voip.Manager, number string) {
	time.Sleep(8 * time.Second) // let the connection settle
	to, err := client.ResolveRecipientJID(ctx, number)
	if err != nil {
		fmt.Printf("❌ resolve %s: %v\n", number, err)
		return
	}
	fmt.Printf("📲 initiating call to %s (%s) — offer handshake only, no audio\n", number, to)
	callID, relays, err := mgr.StartCall(ctx, to)
	if err != nil {
		fmt.Printf("❌ StartCall failed: %v\n", err)
		return
	}
	fmt.Printf("✅ offer handshake ok: callID=%s, %d relay endpoint(s)\n", callID, len(relays.Relays))
	for i, r := range relays.Relays {
		fmt.Printf("   relay %d: %s:%d id=%d name=%s\n", i, r.IP, r.Port, r.RelayID, r.RelayName)
	}
	time.Sleep(2 * time.Second)
	if err := mgr.Terminate(ctx, voip.IncomingCall{CallID: callID, From: to}); err != nil {
		fmt.Printf("⚠️  terminate failed: %v\n", err)
	} else {
		fmt.Printf("✅ terminated call %s\n", callID)
	}
}
