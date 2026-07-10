// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsHandlerExposesCounters(t *testing.T) {
	m := New()
	m.CallStarted("outbound")
	m.CallAnswered()
	m.ActiveInc()
	m.CallEnded("user_ended")
	m.ObserveSetup(2.5)
	m.ObserveDuration(30)

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`voip_calls_started_total{direction="outbound"} 1`,
		"voip_calls_answered_total 1",
		`voip_calls_ended_total{reason="user_ended"} 1`,
		"voip_active_calls 1",
		"voip_call_setup_seconds",
		"voip_call_duration_seconds",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}
