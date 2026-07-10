// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq" // postgres driver ("postgres")
	"github.com/pion/webrtc/v4"

	"github.com/williamprado/whatsmeow/voip/cdr"
)

// openDB opens the session store: Postgres when DATABASE_URL is set (multi-node /
// HA), else the local SQLite file. Returns the *sql.DB and the whatsmeow dialect.
func openDB() (*sql.DB, string, error) {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		db, err := sql.Open("postgres", url)
		return db, "postgres", err
	}
	sessionFile := getenv("SESSION_DB", "examples/voip-bridge-server/session.db")
	db, err := sql.Open("sqlite", "file:"+sessionFile+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(15000)")
	return db, "sqlite3", err
}

// openCDRSink returns a Postgres CDR sink when using Postgres, else a JSON-lines
// file sink (CDR_FILE, default examples/voip-bridge-server/cdr.jsonl).
func openCDRSink(db *sql.DB, usePostgres bool) (cdr.Sink, error) {
	if usePostgres {
		return cdr.NewPostgresSink(db)
	}
	path := getenv("CDR_FILE", "examples/voip-bridge-server/cdr.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return cdr.NewJSONLSink(f), nil
}

// --- P0 hardening: ICE (STUN/TURN) config + HTTP bearer-token auth ---
//
// This is a demo-grade skeleton showing HOW to wire the two P0 items from
// docs/voip_production.md into the bridge. A production deployment integrates the
// same ideas into the authenticated AtendZappy backend.

// iceConfig holds the ICE servers for both legs: the pion (server) form and the
// browser-facing JSON form. When a TURN shared secret is configured, fresh
// ephemeral credentials are minted per call (coturn REST API / "use-auth-secret").
type iceConfig struct {
	stunURLs   []string
	turnURLs   []string
	turnSecret string        // coturn REST shared secret (ephemeral creds)
	turnUser   string        // static TURN username (alternative to secret)
	turnPass   string        // static TURN credential
	turnTTL    time.Duration // ephemeral credential lifetime
}

// iceServerJSON is the browser RTCIceServer shape.
type iceServerJSON struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

func loadICEConfig() iceConfig {
	c := iceConfig{
		stunURLs:   splitCSV(getenv("STUN_URLS", "stun:stun.l.google.com:19302")),
		turnURLs:   splitCSV(os.Getenv("TURN_URLS")),
		turnSecret: os.Getenv("TURN_SECRET"),
		turnUser:   os.Getenv("TURN_USER"),
		turnPass:   os.Getenv("TURN_PASS"),
		turnTTL:    time.Duration(atoiDefault(os.Getenv("TURN_TTL"), 3600)) * time.Second,
	}
	return c
}

// servers builds the ICE server list for one call, minting fresh ephemeral TURN
// credentials if a shared secret is set. Returns both the pion and browser forms
// so the two legs use matching credentials.
func (c iceConfig) servers(now time.Time) ([]webrtc.ICEServer, []iceServerJSON) {
	var pion []webrtc.ICEServer
	var js []iceServerJSON

	if len(c.stunURLs) > 0 {
		pion = append(pion, webrtc.ICEServer{URLs: c.stunURLs})
		js = append(js, iceServerJSON{URLs: c.stunURLs})
	}

	if len(c.turnURLs) > 0 {
		user, cred := c.turnUser, c.turnPass
		if c.turnSecret != "" {
			user, cred = turnEphemeral(c.turnSecret, now, c.turnTTL)
		}
		if user != "" && cred != "" {
			pion = append(pion, webrtc.ICEServer{
				URLs:           c.turnURLs,
				Username:       user,
				Credential:     cred,
				CredentialType: webrtc.ICECredentialTypePassword,
			})
			js = append(js, iceServerJSON{URLs: c.turnURLs, Username: user, Credential: cred})
		}
	}
	return pion, js
}

// turnEphemeral returns a coturn REST-API ephemeral credential pair:
// username = "<expiryUnix>", credential = base64(HMAC-SHA1(secret, username)).
// coturn must run with `use-auth-secret` + `static-auth-secret=<secret>`.
func turnEphemeral(secret string, now time.Time, ttl time.Duration) (user, cred string) {
	expiry := now.Add(ttl).Unix()
	user = strconv.FormatInt(expiry, 10)
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(user))
	cred = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return user, cred
}

// --- auth ---

// authMiddleware requires a bearer token when AUTH_TOKEN is set. Regular API
// calls send it as "Authorization: Bearer <token>"; the SSE stream (EventSource
// cannot set headers) may pass it as "?token=<token>". When AUTH_TOKEN is empty,
// auth is disabled (dev only) — the caller logs a warning.
func authMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" { // auth disabled
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") { // static page is public
			next.ServeHTTP(w, r)
			return
		}
		if !constantTimeEqual(presentedToken(r), token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func presentedToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return r.URL.Query().Get("token")
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// --- small env helpers ---

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return def
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
