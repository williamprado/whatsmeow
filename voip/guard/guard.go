// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package guard is the ban-risk guard-rail for the VoIP subsystem: per-account
// opt-in, a per-account call rate limit, a manual kill switch, and automatic
// disabling of an account when its failure rate (e.g. "error 479" / call
// failures) crosses a threshold. It is the highest-value safety control before
// any production pilot — see docs/voip_production.md (§2 ban risk).
//
// The host gates every place/answer through Allow(account) and feeds failures
// via RecordFailure(account, kind). Guard is host-agnostic and concurrency-safe.
package guard

import (
	"errors"
	"sync"
	"time"
)

var (
	// ErrNotOptedIn: the account is not allowed to use VoIP (opt-in required).
	ErrNotOptedIn = errors.New("guard: account not opted in to VoIP")
	// ErrKilled: the account's kill switch is engaged (manually or auto-tripped).
	ErrKilled = errors.New("guard: account disabled (kill switch)")
	// ErrRateLimited: the account exceeded its call rate limit.
	ErrRateLimited = errors.New("guard: account call rate limit exceeded")
)

// Config tunes the guard. Zero values disable the corresponding control.
type Config struct {
	// OptInRequired: when true, only accounts added via AllowAccount may call.
	OptInRequired bool
	// MaxCallsPerWindow within Window is the per-account call rate limit
	// (0 = unlimited). Both must be > 0 to take effect.
	MaxCallsPerWindow int
	Window            time.Duration
	// FailureThreshold within FailureWindow auto-kills an account (0 = disabled).
	FailureThreshold int
	FailureWindow    time.Duration
}

// Guard enforces the ban-risk controls.
type Guard struct {
	cfg Config
	now func() time.Time

	mu       sync.Mutex
	optIn    map[string]bool
	killed   map[string]string // account -> reason
	calls    map[string][]time.Time
	failures map[string][]time.Time

	onAutoKill func(account, reason string)
}

// New builds a Guard.
func New(cfg Config) *Guard {
	return &Guard{
		cfg:      cfg,
		now:      time.Now,
		optIn:    make(map[string]bool),
		killed:   make(map[string]string),
		calls:    make(map[string][]time.Time),
		failures: make(map[string][]time.Time),
	}
}

// OnAutoKill registers a callback fired when an account is auto-disabled by the
// failure monitor (good for metrics/alerting).
func (g *Guard) OnAutoKill(fn func(account, reason string)) { g.onAutoKill = fn }

// AllowAccount opts an account in; DenyAccount opts it out.
func (g *Guard) AllowAccount(account string) { g.setOptIn(account, true) }
func (g *Guard) DenyAccount(account string)  { g.setOptIn(account, false) }

func (g *Guard) setOptIn(account string, on bool) {
	g.mu.Lock()
	g.optIn[account] = on
	g.mu.Unlock()
}

// Kill manually disables an account; Revive clears the kill switch and its
// recent failure history.
func (g *Guard) Kill(account, reason string) {
	g.mu.Lock()
	g.killed[account] = reason
	g.mu.Unlock()
}

func (g *Guard) Revive(account string) {
	g.mu.Lock()
	delete(g.killed, account)
	delete(g.failures, account)
	g.mu.Unlock()
}

// IsKilled reports whether the account is disabled, and the reason.
func (g *Guard) IsKilled(account string) (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r, ok := g.killed[account]
	return ok, r
}

// Allow gates a place/answer for account. On success it records the call against
// the rate window. Returns a typed error otherwise.
func (g *Guard) Allow(account string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.cfg.OptInRequired && !g.optIn[account] {
		return ErrNotOptedIn
	}
	if _, dead := g.killed[account]; dead {
		return ErrKilled
	}
	if g.cfg.MaxCallsPerWindow > 0 && g.cfg.Window > 0 {
		now := g.now()
		recent := prune(g.calls[account], now.Add(-g.cfg.Window))
		if len(recent) >= g.cfg.MaxCallsPerWindow {
			g.calls[account] = recent
			return ErrRateLimited
		}
		g.calls[account] = append(recent, now)
	}
	return nil
}

// RecordFailure notes a failure for account (e.g. "479", "failed", "timeout").
// If failures cross the configured threshold within the window, the account is
// auto-killed and OnAutoKill fires. Returns true when it tripped the kill switch.
func (g *Guard) RecordFailure(account, kind string) bool {
	if g.cfg.FailureThreshold <= 0 || g.cfg.FailureWindow <= 0 {
		return false
	}
	g.mu.Lock()
	now := g.now()
	recent := append(prune(g.failures[account], now.Add(-g.cfg.FailureWindow)), now)
	g.failures[account] = recent
	_, already := g.killed[account]
	trip := !already && len(recent) >= g.cfg.FailureThreshold
	if trip {
		g.killed[account] = "auto: failure rate (" + kind + ")"
	}
	reason := g.killed[account]
	cb := g.onAutoKill
	g.mu.Unlock()

	if trip && cb != nil {
		cb(account, reason)
	}
	return trip
}

// RecordSuccess clears the recent failure history for account (a good call is a
// signal the account is healthy again).
func (g *Guard) RecordSuccess(account string) {
	g.mu.Lock()
	delete(g.failures, account)
	g.mu.Unlock()
}

// prune drops timestamps at or before cutoff (input is time-ordered).
func prune(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(ts) && !ts[i].After(cutoff) {
		i++
	}
	if i == 0 {
		return ts
	}
	out := make([]time.Time, len(ts)-i)
	copy(out, ts[i:])
	return out
}
