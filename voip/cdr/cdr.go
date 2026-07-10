// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package cdr records Call Detail Records for the VoIP subsystem: one row per
// call with direction, peer, timestamps, duration, and end reason — for billing,
// audit, and quality analysis. See docs/voip_production.md (P0: CDR).
//
// A Recorder tracks in-flight calls (driven by the voip.Manager lifecycle
// callbacks) and writes a finished Record to a Sink on call end. Sinks provided:
// JSONLSink (JSON lines to a file) and PostgresSink (see cdr_postgres.go).
package cdr

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Record is one completed call.
type Record struct {
	CallID      string    `json:"call_id"`
	Account     string    `json:"account"`   // our own JID/number
	Direction   string    `json:"direction"` // inbound | outbound
	Peer        string    `json:"peer"`
	StartedAt   time.Time `json:"started_at"`
	AnsweredAt  time.Time `json:"answered_at,omitempty"`
	EndedAt     time.Time `json:"ended_at"`
	SetupSec    float64   `json:"setup_sec"`    // started -> answered
	DurationSec float64   `json:"duration_sec"` // answered -> ended (0 if unanswered)
	Answered    bool      `json:"answered"`
	EndReason   string    `json:"end_reason"`
}

// Sink persists finished records.
type Sink interface {
	Write(Record) error
	Close() error
}

// Recorder tracks in-flight calls and emits a Record per call on Ended.
type Recorder struct {
	sink Sink
	now  func() time.Time

	mu       sync.Mutex
	inFlight map[string]*Record
}

// NewRecorder writes finished records to sink. Safe for concurrent use.
func NewRecorder(sink Sink) *Recorder {
	return &Recorder{sink: sink, now: time.Now, inFlight: make(map[string]*Record)}
}

// Started registers a new call (inbound rings, or outbound placed).
func (r *Recorder) Started(callID, direction, peer, account string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.inFlight[callID]; ok {
		return
	}
	r.inFlight[callID] = &Record{
		CallID:    callID,
		Account:   account,
		Direction: direction,
		Peer:      peer,
		StartedAt: r.now(),
	}
}

// Answered marks the call active (idempotent).
func (r *Recorder) Answered(callID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.inFlight[callID]
	if rec == nil || rec.Answered {
		return
	}
	rec.Answered = true
	rec.AnsweredAt = r.now()
	rec.SetupSec = rec.AnsweredAt.Sub(rec.StartedAt).Seconds()
}

// Ended finalizes the call and writes the Record. It returns the completed
// Record and ok=false if the call was unknown (already ended / never started).
func (r *Recorder) Ended(callID, reason string) (Record, bool, error) {
	r.mu.Lock()
	rec := r.inFlight[callID]
	if rec == nil {
		r.mu.Unlock()
		return Record{}, false, nil
	}
	delete(r.inFlight, callID)
	r.mu.Unlock()

	rec.EndedAt = r.now()
	rec.EndReason = reason
	if rec.Answered {
		rec.DurationSec = rec.EndedAt.Sub(rec.AnsweredAt).Seconds()
	}
	return *rec, true, r.sink.Write(*rec)
}

// JSONLSink writes one JSON object per line to an io.WriteCloser (e.g. a file).
type JSONLSink struct {
	mu  sync.Mutex
	w   io.WriteCloser
	enc *json.Encoder
}

// NewJSONLSink writes records as JSON lines to w.
func NewJSONLSink(w io.WriteCloser) *JSONLSink {
	return &JSONLSink{w: w, enc: json.NewEncoder(w)}
}

func (s *JSONLSink) Write(rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enc.Encode(rec) // Encoder adds a trailing newline
}

func (s *JSONLSink) Close() error { return s.w.Close() }
