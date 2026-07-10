// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Command voip-bridge-server is a slim, single-session HTTP server that bridges
// a WhatsApp voice call to a human operator in the browser over WebRTC. It logs
// in one WhatsApp account (QR on first run) and serves an agent page at "/".
//
// Flow: the browser opens a WebRTC data channel carrying 16 kHz mono PCM. The
// operator's mic PCM is fed into the WhatsApp call (voip.Manager.FeedCapturedPCM)
// and the WhatsApp peer's audio (OnPeerAudio) is sent back to the browser.
//
// ⚠️ VERY HIGH account-ban risk. Disposable accounts only, never production.
//
// Usage:
//
//	VOIP_ENABLED=1 go run ./examples/voip-bridge-server      # then open http://localhost:8080
//
// Env: VOIP_ENABLED ("1" required to place/answer), ADDR (default :8080),
// SESSION_DB (default examples/voip-bridge-server/session.db).
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	_ "embed"

	"github.com/mdp/qrterminal/v3"
	_ "modernc.org/sqlite"

	"github.com/williamprado/whatsmeow/voip"
	"github.com/williamprado/whatsmeow/voip/bridge"
	"github.com/williamprado/whatsmeow/voip/call"
	"github.com/williamprado/whatsmeow/voip/core"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

//go:embed agent.html
var agentHTML []byte

type appServer struct {
	mgr *voip.Manager
	log *slog.Logger

	mu      sync.Mutex
	bridges map[string]*bridge.Bridge // callID -> browser bridge
	subs    map[chan sseEvent]struct{}
}

type sseEvent struct {
	Type   string `json:"type"`
	CallID string `json:"call_id,omitempty"`
	Peer   string `json:"peer,omitempty"`
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	enabled := os.Getenv("VOIP_ENABLED") == "1"
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	sessionFile := os.Getenv("SESSION_DB")
	if sessionFile == "" {
		sessionFile = "examples/voip-bridge-server/session.db"
	}

	fmt.Println("⚠️  VERY HIGH BAN RISK: scan the QR ONLY with a disposable account. NEVER production.")
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+sessionFile+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(15000)")
	if err != nil {
		return fmt.Errorf("open session db: %w", err)
	}
	defer db.Close()
	container := sqlstore.NewWithDB(db, "sqlite3", waLog.Stdout("Database", "INFO", true))
	if err = container.Upgrade(ctx); err != nil {
		return fmt.Errorf("upgrade session db: %w", err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("get device: %w", err)
	}
	client := whatsmeow.NewClient(device, waLog.Stdout("Client", "INFO", true))

	app := &appServer{
		mgr:     voip.New(client, voip.Config{Enabled: enabled, MaxConcurrentCalls: 1}, slog.Default()),
		log:     slog.Default(),
		bridges: make(map[string]*bridge.Bridge),
		subs:    make(map[chan sseEvent]struct{}),
	}

	app.mgr.OnIncomingCall(func(c *call.CallInfo) {
		app.broadcast(sseEvent{Type: "incoming", CallID: c.CallID, Peer: c.PeerJid})
	})
	app.mgr.OnCallStateChange(func(c *call.CallInfo) {
		app.broadcast(sseEvent{Type: "state", CallID: c.CallID, State: string(c.StateData.State)})
	})
	app.mgr.OnCallEnded(func(c *call.CallInfo) {
		app.closeBridge(c.CallID)
		app.broadcast(sseEvent{Type: "ended", CallID: c.CallID, Reason: string(c.StateData.EndReason)})
	})
	app.mgr.OnPeerAudio(func(callID string, pcm16 []float32) {
		if b := app.getBridge(callID); b != nil {
			_ = b.WritePCM(pcm16)
		}
	})

	if err := app.connect(ctx, client); err != nil {
		return err
	}
	app.mgr.Start()
	defer app.mgr.Stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", app.handleIndex)
	mux.HandleFunc("GET /api/events", app.handleEvents)
	mux.HandleFunc("POST /api/call", app.handleStartCall)
	mux.HandleFunc("POST /api/call/{id}/webrtc", app.handleWebRTC)
	mux.HandleFunc("POST /api/call/{id}/accept", app.handleAccept)
	mux.HandleFunc("POST /api/call/{id}/reject", app.handleReject)
	mux.HandleFunc("DELETE /api/call/{id}", app.handleEndCall)

	fmt.Printf("✅ bridge server on http://localhost%s (enabled=%v) — open it in a browser\n", addr, enabled)
	return http.ListenAndServe(addr, mux)
}

func (app *appServer) connect(ctx context.Context, client *whatsmeow.Client) error {
	if client.Store.ID != nil {
		return client.Connect()
	}
	qrChan, _ := client.GetQRChannel(ctx)
	if err := client.Connect(); err != nil {
		return err
	}
	go func() {
		for evt := range qrChan {
			if evt.Event == "code" {
				fmt.Println("scan this QR with a DISPOSABLE account:")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
			}
		}
	}()
	return nil
}

// --- HTTP handlers ---

func (app *appServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(agentHTML)
}

func (app *appServer) handleStartCall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.To == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "to (phone number, digits only) required"})
		return
	}
	peer := types.NewJID(body.To, types.DefaultUserServer)
	callID, err := app.mgr.StartCall(r.Context(), peer, false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"call_id": callID})
}

func (app *appServer) handleWebRTC(w http.ResponseWriter, r *http.Request) {
	callID := r.PathValue("id")
	var body struct {
		SDPOffer string `json:"sdp_offer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SDPOffer == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sdp_offer required"})
		return
	}
	br, answer, err := bridge.New(body.SDPOffer, app.log)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	br.OnBrowserPCM = func(pcm []float32) { _ = app.mgr.FeedCapturedPCM(callID, pcm) }
	br.OnTerminalICE = func() { go app.mgr.EndCall(context.Background(), callID, core.EndCallReasonUserEnded) }
	app.setBridge(callID, br)
	writeJSON(w, http.StatusOK, map[string]string{"sdp_answer": answer})
}

func (app *appServer) handleAccept(w http.ResponseWriter, r *http.Request) {
	if err := app.mgr.AcceptCall(r.Context(), r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (app *appServer) handleReject(w http.ResponseWriter, r *http.Request) {
	if err := app.mgr.RejectCall(r.Context(), r.PathValue("id"), core.EndCallReasonDeclined); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (app *appServer) handleEndCall(w http.ResponseWriter, r *http.Request) {
	callID := r.PathValue("id")
	_ = app.mgr.EndCall(r.Context(), callID, core.EndCallReasonUserEnded)
	app.closeBridge(callID)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (app *appServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan sseEvent, 16)
	app.mu.Lock()
	app.subs[ch] = struct{}{}
	app.mu.Unlock()
	defer func() {
		app.mu.Lock()
		delete(app.subs, ch)
		app.mu.Unlock()
	}()

	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case evt := <-ch:
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// --- helpers ---

func (app *appServer) setBridge(callID string, b *bridge.Bridge) {
	app.mu.Lock()
	if old := app.bridges[callID]; old != nil {
		old.Close()
	}
	app.bridges[callID] = b
	app.mu.Unlock()
}

func (app *appServer) getBridge(callID string) *bridge.Bridge {
	app.mu.Lock()
	defer app.mu.Unlock()
	return app.bridges[callID]
}

func (app *appServer) closeBridge(callID string) {
	app.mu.Lock()
	b := app.bridges[callID]
	delete(app.bridges, callID)
	app.mu.Unlock()
	if b != nil {
		b.Close()
	}
}

func (app *appServer) broadcast(evt sseEvent) {
	app.mu.Lock()
	defer app.mu.Unlock()
	for ch := range app.subs {
		select {
		case ch <- evt:
		default:
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
