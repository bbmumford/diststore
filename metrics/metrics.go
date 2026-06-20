/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Package metrics provides a nil-safe Sink interface so diststore can
// record counters + histograms without dragging in a Prometheus / OTel
// dependency. Concrete adapters live in the consumer.
package metrics

// Counter records a monotonically-increasing tally.
type Counter interface {
	Inc()
	Add(n float64)
}

// Histogram records a distribution of observations.
type Histogram interface {
	Observe(value float64)
}

// Sink is the small surface diststore uses to publish metrics. A nil
// Sink is valid and drops everything on the floor.
type Sink interface {
	Counter(name string, labels ...string) Counter
	Histogram(name string, labels ...string) Histogram
}

// Nop returns a Sink that drops every record on the floor.
func Nop() Sink { return nopSink{} }

type nopSink struct{}

func (nopSink) Counter(_ string, _ ...string) Counter     { return nopCounter{} }
func (nopSink) Histogram(_ string, _ ...string) Histogram { return nopHist{} }

type nopCounter struct{}

func (nopCounter) Inc()          {}
func (nopCounter) Add(_ float64) {}

type nopHist struct{}

func (nopHist) Observe(_ float64) {}
