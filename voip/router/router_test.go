// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package router

import (
	"strconv"
	"testing"
)

func accounts(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "55779999" + strconv.Itoa(10000+i) + "@s.whatsapp.net"
	}
	return out
}

func TestEmptyRing(t *testing.T) {
	r := NewRing(0)
	if got := r.Owner("acct"); got != "" {
		t.Fatalf("empty ring owner = %q, want \"\"", got)
	}
}

func TestDeterministicAcrossBuildOrder(t *testing.T) {
	a := NewRing(64, "w1", "w2", "w3")
	b := NewRing(64, "w3", "w1", "w2")
	c := NewRing(64)
	c.Add("w2")
	c.Add("w3")
	c.Add("w1")
	for _, acct := range accounts(500) {
		if a.Owner(acct) != b.Owner(acct) || a.Owner(acct) != c.Owner(acct) {
			t.Fatalf("owner of %s differs across build orders: %q / %q / %q",
				acct, a.Owner(acct), b.Owner(acct), c.Owner(acct))
		}
	}
}

func TestDistribution(t *testing.T) {
	workers := []string{"w1", "w2", "w3", "w4", "w5"}
	r := NewRing(0, workers...)
	counts := map[string]int{}
	for _, acct := range accounts(2000) {
		counts[r.Owner(acct)]++
	}
	for _, w := range workers {
		if counts[w] == 0 {
			t.Fatalf("worker %s owns no accounts: %v", w, counts)
		}
	}
}

func TestRemoveOnlyRemapsRemovedWorker(t *testing.T) {
	r := NewRing(0, "w1", "w2", "w3", "w4")
	accts := accounts(1000)
	before := make(map[string]string, len(accts))
	for _, a := range accts {
		before[a] = r.Owner(a)
	}

	r.Remove("w3")
	for _, a := range accts {
		after := r.Owner(a)
		if after == "w3" {
			t.Fatalf("account %s still owned by removed worker", a)
		}
		if before[a] != "w3" && after != before[a] {
			t.Fatalf("account %s moved from %s to %s but its worker was not removed",
				a, before[a], after)
		}
	}
}

func TestOwnsAndWorkers(t *testing.T) {
	r := NewRing(0, "b", "a")
	if got := r.Workers(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("Workers() = %v", got)
	}
	acct := "5577988272902@s.whatsapp.net"
	owner := r.Owner(acct)
	if !r.Owns(owner, acct) {
		t.Fatalf("Owns(%q) = false for the ring's own answer", owner)
	}
}
