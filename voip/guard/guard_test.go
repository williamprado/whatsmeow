// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package guard

import (
	"errors"
	"testing"
	"time"
)

const acct = "5511@s.whatsapp.net"

func TestOptIn(t *testing.T) {
	g := New(Config{OptInRequired: true})
	if err := g.Allow(acct); !errors.Is(err, ErrNotOptedIn) {
		t.Fatalf("Allow before opt-in = %v, want ErrNotOptedIn", err)
	}
	g.AllowAccount(acct)
	if err := g.Allow(acct); err != nil {
		t.Fatalf("Allow after opt-in = %v", err)
	}
	g.DenyAccount(acct)
	if err := g.Allow(acct); !errors.Is(err, ErrNotOptedIn) {
		t.Errorf("Allow after deny = %v, want ErrNotOptedIn", err)
	}
}

func TestRateLimit(t *testing.T) {
	g := New(Config{MaxCallsPerWindow: 2, Window: time.Minute})
	clock := time.Unix(1700000000, 0)
	g.now = func() time.Time { return clock }

	if err := g.Allow(acct); err != nil {
		t.Fatal(err)
	}
	if err := g.Allow(acct); err != nil {
		t.Fatal(err)
	}
	if err := g.Allow(acct); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("3rd call = %v, want ErrRateLimited", err)
	}
	// After the window slides, calls are allowed again.
	clock = clock.Add(61 * time.Second)
	if err := g.Allow(acct); err != nil {
		t.Errorf("after window = %v, want nil", err)
	}
}

func TestManualKill(t *testing.T) {
	g := New(Config{})
	g.Kill(acct, "manual")
	if dead, reason := g.IsKilled(acct); !dead || reason != "manual" {
		t.Fatalf("IsKilled = %v/%q", dead, reason)
	}
	if err := g.Allow(acct); !errors.Is(err, ErrKilled) {
		t.Errorf("Allow when killed = %v, want ErrKilled", err)
	}
	g.Revive(acct)
	if err := g.Allow(acct); err != nil {
		t.Errorf("Allow after revive = %v", err)
	}
}

func TestAutoKillOnFailures(t *testing.T) {
	g := New(Config{FailureThreshold: 3, FailureWindow: time.Minute})
	clock := time.Unix(1700000000, 0)
	g.now = func() time.Time { return clock }

	var killedAcct, killedReason string
	g.OnAutoKill(func(a, r string) { killedAcct, killedReason = a, r })

	if g.RecordFailure(acct, "479") || g.RecordFailure(acct, "479") {
		t.Fatal("should not trip before threshold")
	}
	if !g.RecordFailure(acct, "479") {
		t.Fatal("3rd failure should trip the kill switch")
	}
	if killedAcct != acct || killedReason == "" {
		t.Errorf("OnAutoKill got %q/%q", killedAcct, killedReason)
	}
	if err := g.Allow(acct); !errors.Is(err, ErrKilled) {
		t.Errorf("Allow after auto-kill = %v, want ErrKilled", err)
	}
	// Failures outside the window don't accumulate.
	g.Revive(acct)
	g.RecordFailure(acct, "479")
	clock = clock.Add(2 * time.Minute)
	g.RecordFailure(acct, "479")
	if dead, _ := g.IsKilled(acct); dead {
		t.Error("stale failures should not trip the switch")
	}
}
