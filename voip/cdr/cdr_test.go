// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package cdr

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

type memSink struct{ recs []Record }

func (m *memSink) Write(r Record) error { m.recs = append(m.recs, r); return nil }
func (m *memSink) Close() error         { return nil }

func TestRecorderAnsweredLifecycle(t *testing.T) {
	sink := &memSink{}
	r := NewRecorder(sink)
	base := time.Unix(1700000000, 0)
	clock := base
	r.now = func() time.Time { return clock }

	r.Started("C1", "outbound", "5577999999999", "5511@s.whatsapp.net")
	clock = base.Add(3 * time.Second)
	r.Answered("C1")
	clock = base.Add(3*time.Second + 42*time.Second)
	rec, ok, err := r.Ended("C1", "user_ended")
	if err != nil || !ok {
		t.Fatalf("Ended = ok:%v err:%v", ok, err)
	}
	if !rec.Answered || rec.SetupSec != 3 || rec.DurationSec != 42 {
		t.Errorf("setup=%v dur=%v answered=%v, want 3/42/true", rec.SetupSec, rec.DurationSec, rec.Answered)
	}
	if rec.Direction != "outbound" || rec.Peer != "5577999999999" || rec.EndReason != "user_ended" {
		t.Errorf("record fields wrong: %+v", rec)
	}
	if len(sink.recs) != 1 {
		t.Fatalf("sink got %d records, want 1", len(sink.recs))
	}
}

func TestRecorderUnanswered(t *testing.T) {
	sink := &memSink{}
	r := NewRecorder(sink)
	r.Started("C2", "inbound", "peer", "acct")
	rec, ok, _ := r.Ended("C2", "declined")
	if !ok || rec.Answered || rec.DurationSec != 0 {
		t.Errorf("unanswered: ok=%v answered=%v dur=%v", ok, rec.Answered, rec.DurationSec)
	}
}

func TestRecorderUnknownCall(t *testing.T) {
	r := NewRecorder(&memSink{})
	if _, ok, _ := r.Ended("nope", "x"); ok {
		t.Error("Ended of unknown call should be ok=false")
	}
}

func TestJSONLSink(t *testing.T) {
	var buf nopWriteCloser
	s := NewJSONLSink(&buf)
	if err := s.Write(Record{CallID: "C9", Direction: "inbound"}); err != nil {
		t.Fatal(err)
	}
	var got Record
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &got); err != nil {
		t.Fatalf("not valid json line: %v", err)
	}
	if got.CallID != "C9" {
		t.Errorf("call id = %q", got.CallID)
	}
}

type nopWriteCloser struct{ bytes.Buffer }

func (nopWriteCloser) Close() error { return nil }
