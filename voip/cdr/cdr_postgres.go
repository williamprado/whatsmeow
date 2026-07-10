// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package cdr

import "database/sql"

// PostgresSink writes CDR rows to a Postgres table. It ensures the table exists
// on construction. The caller owns the *sql.DB (Close here is a no-op).
type PostgresSink struct {
	db *sql.DB
}

const cdrSchema = `
CREATE TABLE IF NOT EXISTS voip_cdr (
    call_id      text PRIMARY KEY,
    account      text,
    direction    text,
    peer         text,
    started_at   timestamptz,
    answered_at  timestamptz,
    ended_at     timestamptz,
    setup_sec    double precision,
    duration_sec double precision,
    answered     boolean,
    end_reason   text
);`

// NewPostgresSink ensures the voip_cdr table exists and returns the sink.
func NewPostgresSink(db *sql.DB) (*PostgresSink, error) {
	if _, err := db.Exec(cdrSchema); err != nil {
		return nil, err
	}
	return &PostgresSink{db: db}, nil
}

func (s *PostgresSink) Write(rec Record) error {
	var answeredAt any
	if rec.Answered {
		answeredAt = rec.AnsweredAt
	} else {
		answeredAt = nil
	}
	_, err := s.db.Exec(`
        INSERT INTO voip_cdr
            (call_id, account, direction, peer, started_at, answered_at, ended_at,
             setup_sec, duration_sec, answered, end_reason)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
        ON CONFLICT (call_id) DO NOTHING`,
		rec.CallID, rec.Account, rec.Direction, rec.Peer,
		rec.StartedAt, answeredAt, rec.EndedAt,
		rec.SetupSec, rec.DurationSec, rec.Answered, rec.EndReason,
	)
	return err
}

// Close is a no-op: the caller owns the *sql.DB.
func (s *PostgresSink) Close() error { return nil }

var _ Sink = (*PostgresSink)(nil)
