// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package metrics provides Prometheus instrumentation for the VoIP subsystem.
// It is host-agnostic: whatever owns the voip.Manager (the demo server or the
// AtendZappy backend) wires the Manager's lifecycle callbacks to these methods
// and exposes Handler() for scraping.
//
// See docs/voip_production.md (P0: observability).
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the VoIP Prometheus collectors, registered on their own
// registry so a host can expose them independently of the default one.
type Metrics struct {
	reg *prometheus.Registry

	callsStarted    *prometheus.CounterVec // by direction
	callsAnswered   prometheus.Counter
	callsEnded      *prometheus.CounterVec // by reason
	callErrors      *prometheus.CounterVec // by op
	activeCalls     prometheus.Gauge
	setupLatency    prometheus.Histogram // offer -> active, seconds
	callDuration    prometheus.Histogram // active -> ended, seconds
	peerAudioFrames prometheus.Counter
}

// New builds and registers the collectors.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		reg: reg,
		callsStarted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "voip_calls_started_total",
			Help: "Calls started, by direction (inbound/outbound).",
		}, []string{"direction"}),
		callsAnswered: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "voip_calls_answered_total",
			Help: "Calls that reached the active state.",
		}),
		callsEnded: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "voip_calls_ended_total",
			Help: "Calls ended, by reason.",
		}, []string{"reason"}),
		callErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "voip_call_errors_total",
			Help: "Call operation errors, by operation.",
		}, []string{"op"}),
		activeCalls: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "voip_active_calls",
			Help: "Currently active calls.",
		}),
		setupLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "voip_call_setup_seconds",
			Help:    "Time from call start to active, seconds.",
			Buckets: []float64{0.5, 1, 2, 3, 5, 8, 13, 21, 34},
		}),
		callDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "voip_call_duration_seconds",
			Help:    "Call duration (active to ended), seconds.",
			Buckets: []float64{5, 15, 30, 60, 120, 300, 600, 1800},
		}),
		peerAudioFrames: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "voip_peer_audio_frames_total",
			Help: "Decoded peer audio frames delivered.",
		}),
	}
	reg.MustRegister(
		m.callsStarted, m.callsAnswered, m.callsEnded, m.callErrors,
		m.activeCalls, m.setupLatency, m.callDuration, m.peerAudioFrames,
	)
	return m
}

// Handler exposes the metrics for Prometheus to scrape. In production put this
// on an internal port or behind scraper auth.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// Registry exposes the underlying registry (e.g. to merge into a host registry).
func (m *Metrics) Registry() *prometheus.Registry { return m.reg }

func (m *Metrics) CallStarted(direction string)    { m.callsStarted.WithLabelValues(direction).Inc() }
func (m *Metrics) CallAnswered()                   { m.callsAnswered.Inc() }
func (m *Metrics) CallEnded(reason string)         { m.callsEnded.WithLabelValues(reason).Inc() }
func (m *Metrics) CallError(op string)             { m.callErrors.WithLabelValues(op).Inc() }
func (m *Metrics) ActiveInc()                      { m.activeCalls.Inc() }
func (m *Metrics) ActiveDec()                      { m.activeCalls.Dec() }
func (m *Metrics) ObserveSetup(seconds float64)    { m.setupLatency.Observe(seconds) }
func (m *Metrics) ObserveDuration(seconds float64) { m.callDuration.Observe(seconds) }
func (m *Metrics) PeerAudioFrame()                 { m.peerAudioFrames.Inc() }
