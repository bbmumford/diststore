/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package trace provides an optional Tracer hook so diststore consumers
// can attach OpenTelemetry (or equivalent) tracing without diststore
// importing the otel SDK directly.
package trace

import "context"

// Span is a no-op span returned by the default Tracer.
type Span interface {
	End()
	SetAttr(key string, value any)
	RecordError(err error)
}

// Tracer is the minimum surface diststore uses. nil = no-op.
type Tracer interface {
	Start(ctx context.Context, name string) (context.Context, Span)
}

// Nop returns a Tracer that produces no-op spans.
func Nop() Tracer { return nopTracer{} }

type nopTracer struct{}

func (nopTracer) Start(ctx context.Context, _ string) (context.Context, Span) {
	return ctx, nopSpan{}
}

type nopSpan struct{}

func (nopSpan) End()                    {}
func (nopSpan) SetAttr(_ string, _ any) {}
func (nopSpan) RecordError(_ error)     {}
