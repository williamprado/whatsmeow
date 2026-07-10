// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package router provides session-affinity routing for VoIP workers.
//
// A WhatsApp account is one Signal device session: a call for account A can
// only be handled by the worker process holding A's session, so calls cannot
// be freely load-balanced (docs/voip_production.md §0). This package maps
// account → worker with a consistent-hash ring so that:
//
//   - every node (API layer or worker) computes the same owner with no
//     coordination beyond the shared worker list, and
//   - adding/removing a worker only remaps the accounts owned by that worker,
//     not the whole fleet.
//
// The ring is deterministic: the same worker set always yields the same
// assignment, regardless of the order workers were added.
package router

import (
	"hash/fnv"
	"sort"
	"strconv"
	"sync"
)

// DefaultReplicas is the number of virtual nodes per worker. More replicas
// smooth the distribution at the cost of a larger ring.
const DefaultReplicas = 128

// Ring is a consistent-hash ring mapping account ids to worker ids.
// Safe for concurrent use.
type Ring struct {
	mu       sync.RWMutex
	replicas int
	workers  map[string]struct{}
	keys     []uint32          // sorted virtual-node hashes
	owner    map[uint32]string // virtual-node hash → worker
}

// NewRing builds a ring with the given virtual-node count per worker
// (replicas <= 0 uses DefaultReplicas) and initial worker set.
func NewRing(replicas int, workers ...string) *Ring {
	if replicas <= 0 {
		replicas = DefaultReplicas
	}
	r := &Ring{replicas: replicas, workers: make(map[string]struct{})}
	for _, w := range workers {
		r.workers[w] = struct{}{}
	}
	r.rebuild()
	return r
}

// Add inserts a worker into the ring. No-op if already present.
func (r *Ring) Add(worker string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.workers[worker]; ok {
		return
	}
	r.workers[worker] = struct{}{}
	r.rebuild()
}

// Remove deletes a worker from the ring. No-op if absent.
func (r *Ring) Remove(worker string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.workers[worker]; !ok {
		return
	}
	delete(r.workers, worker)
	r.rebuild()
}

// Owner returns the worker responsible for the given account, or "" when the
// ring is empty.
func (r *Ring) Owner(account string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.keys) == 0 {
		return ""
	}
	h := hash32(account)
	i := sort.Search(len(r.keys), func(i int) bool { return r.keys[i] >= h })
	if i == len(r.keys) { // wrap around
		i = 0
	}
	return r.owner[r.keys[i]]
}

// Owns reports whether worker is the owner of account.
func (r *Ring) Owns(worker, account string) bool {
	return r.Owner(account) == worker
}

// Workers returns the current worker set, sorted.
func (r *Ring) Workers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.workers))
	for w := range r.workers {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// rebuild recomputes the virtual-node table. Called with r.mu held. Iterating
// workers in sorted order makes hash collisions resolve deterministically.
func (r *Ring) rebuild() {
	names := make([]string, 0, len(r.workers))
	for w := range r.workers {
		names = append(names, w)
	}
	sort.Strings(names)

	r.keys = r.keys[:0]
	r.owner = make(map[uint32]string, len(names)*r.replicas)
	for _, w := range names {
		for i := 0; i < r.replicas; i++ {
			h := hash32(w + "#" + strconv.Itoa(i))
			if _, taken := r.owner[h]; taken {
				continue // rare collision: first (sorted) worker keeps the slot
			}
			r.owner[h] = w
			r.keys = append(r.keys, h)
		}
	}
	sort.Slice(r.keys, func(i, j int) bool { return r.keys[i] < r.keys[j] })
}

func hash32(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}
