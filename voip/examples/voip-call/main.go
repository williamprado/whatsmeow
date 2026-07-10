// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Command voip-call is a manual smoke test for the VoIP media stack (MLow codec
// + SRTP + relay transport) ported from williamprado/WaCalls. It logs in via QR
// and can either wait for an incoming call or place an outgoing one, exchanging
// real audio over the WhatsApp relay.
//
// ⚠️ VERY HIGH ACCOUNT-BAN RISK. Placing/answering calls from a non-official
// library uses reverse-engineered crypto and protocol constants. Scan the QR
// ONLY with a disposable account. NEVER the production (atendzappy) account.
//
// Usage:
//
//	# Receive: wait for a call and auto-answer, feeding a test tone.
//	VOIP_ENABLED=1 VOIP_ACCEPT=1 go run ./examples/voip-call
//
//	# Place a call to a 2nd disposable number for VOIP_SECS seconds.
//	VOIP_ENABLED=1 VOIP_CALL=557798020125 VOIP_SECS=15 go run ./examples/voip-call
//
// Env:
//
//	VOIP_ENABLED  "1" to actually set up media (default: detect + reject only)
//	VOIP_CALL     destination phone number (digits only) to place an outbound call
//	VOIP_ACCEPT   "1" to auto-answer inbound calls (otherwise they are rejected)
//	VOIP_SECS     outbound call duration in seconds (default 10)
//	SESSION_DB    session file path (default examples/voip-call/session.db)
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mdp/qrterminal/v3"
	_ "modernc.org/sqlite"

	"github.com/williamprado/whatsmeow/voip"
	"github.com/williamprado/whatsmeow/voip/call"
	"github.com/williamprado/whatsmeow/voip/core"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	enabled := os.Getenv("VOIP_ENABLED") == "1"
	autoAccept := os.Getenv("VOIP_ACCEPT") == "1"
	callTarget := os.Getenv("VOIP_CALL")
	secs := 10
	if v, err := strconv.Atoi(os.Getenv("VOIP_SECS")); err == nil && v > 0 {
		secs = v
	}
	sessionFile := os.Getenv("SESSION_DB")
	if sessionFile == "" {
		sessionFile = "examples/voip-call/session.db"
	}

	fmt.Println("⚠️  VERY HIGH BAN RISK: scan the QR ONLY with a disposable account. NEVER the production account.")
	fmt.Printf("    enabled=%v autoAccept=%v callTarget=%q secs=%d\n", enabled, autoAccept, callTarget, secs)

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
	client := whatsmeow.NewClient(device, waLog.Stdout("Client", "INFO", true))

	// VoIP manager.
	mgr := voip.New(client, voip.Config{Enabled: enabled, MaxConcurrentCalls: 1}, slog.Default())

	var peerFrames atomic.Int64
	mgr.OnPeerAudio(func(callID string, pcm16 []float32) {
		if n := peerFrames.Add(1); n%50 == 0 { // ~1s at 20ms frames
			fmt.Printf("🔊 peer audio: %d frames received (last frame %d samples)\n", n, len(pcm16))
		}
	})
	mgr.OnCallStateChange(func(c *call.CallInfo) {
		fmt.Printf("📞 call %s -> %s\n", c.CallID, c.StateData.State)
	})
	mgr.OnCallEnded(func(c *call.CallInfo) {
		fmt.Printf("🔚 call %s ended: %s (peer frames=%d)\n", c.CallID, c.StateData.EndReason, peerFrames.Load())
	})
	mgr.OnIncomingCall(func(c *call.CallInfo) {
		fmt.Printf("📲 incoming call %s from %s\n", c.CallID, c.PeerJid)
		if !enabled {
			return
		}
		go func() {
			if autoAccept {
				if err := mgr.AcceptCall(context.Background(), c.CallID); err != nil {
					fmt.Printf("accept error: %v\n", err)
					return
				}
				feedTone(mgr, c.CallID, secs)
				_ = mgr.EndCall(context.Background(), c.CallID, core.EndCallReasonUserEnded)
			} else {
				_ = mgr.RejectCall(context.Background(), c.CallID, core.EndCallReasonDeclined)
				fmt.Printf("✅ rejected %s\n", c.CallID)
			}
		}()
	})

	loggedIn := make(chan struct{}, 1)
	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(ctx)
		if err := client.Connect(); err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		go func() {
			for evt := range qrChan {
				switch evt.Event {
				case "code":
					qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				case "success":
					select {
					case loggedIn <- struct{}{}:
					default:
					}
				}
			}
		}()
	} else {
		if err := client.Connect(); err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		select {
		case loggedIn <- struct{}{}:
		default:
		}
	}

	select {
	case <-loggedIn:
	case <-time.After(90 * time.Second):
		return fmt.Errorf("timed out waiting for login")
	}
	fmt.Println("✅ logged in")
	mgr.Start()
	defer mgr.Stop()

	// Give post-login sync a moment to settle before touching the call subsystem.
	time.Sleep(6 * time.Second)

	if callTarget != "" {
		if !enabled {
			return fmt.Errorf("VOIP_CALL set but VOIP_ENABLED != 1 — refusing to place a call")
		}
		peer := types.NewJID(callTarget, types.DefaultUserServer)
		callID, err := mgr.StartCall(ctx, peer, false)
		if err != nil {
			return fmt.Errorf("start call: %w", err)
		}
		fmt.Printf("📤 placed call %s to %s — streaming a test tone for %ds\n", callID, peer, secs)
		feedTone(mgr, callID, secs)
		_ = mgr.EndCall(ctx, callID, core.EndCallReasonUserEnded)
		fmt.Println("done.")
		time.Sleep(1 * time.Second)
		return nil
	}

	// Receive mode: wait for Ctrl-C.
	fmt.Println("waiting for incoming calls… press Ctrl-C to quit")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	return nil
}

// feedTone streams a low-amplitude 440 Hz sine as 16 kHz mono float32 PCM in
// 20 ms frames (320 samples) for the given number of seconds, exercising the
// MLow encoder → SRTP → relay path with real (non-silent) audio.
func feedTone(mgr *voip.Manager, callID string, secs int) {
	const (
		sampleRate = 16000
		frameLen   = 320 // 20 ms
		freq       = 440.0
		amp        = 0.2
	)
	frames := secs * sampleRate / frameLen
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	phase := 0.0
	step := 2 * math.Pi * freq / sampleRate
	for i := 0; i < frames; i++ {
		<-ticker.C
		buf := make([]float32, frameLen)
		for j := range buf {
			buf[j] = float32(amp * math.Sin(phase))
			phase += step
			if phase > 2*math.Pi {
				phase -= 2 * math.Pi
			}
		}
		if err := mgr.FeedCapturedPCM(callID, buf); err != nil {
			return // call ended
		}
	}
}
