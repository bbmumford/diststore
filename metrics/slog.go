/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package metrics

import (
	"context"
	"log/slog"
)

// Subsystem is the slog attribute key + value diststore tags every log
// record with so operators can grep across components.
const Subsystem = "diststore"

// Log returns a slog.Logger pre-tagged with subsystem=<Subsystem> and
// component=<comp>. Pass an empty comp to tag only the subsystem.
func Log(comp string) *slog.Logger {
	l := slog.Default().With(slog.String("subsystem", Subsystem))
	if comp != "" {
		l = l.With(slog.String("component", comp))
	}
	return l
}

// LogCtx is like Log but attaches request-scoped attrs from ctx if any
// are present. The function is intentionally tiny so call sites stay
// readable.
func LogCtx(ctx context.Context, comp string) *slog.Logger {
	_ = ctx // reserved for future ctx-attributed logging
	return Log(comp)
}
