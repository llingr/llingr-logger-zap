// SPDX-FileCopyrightText: Copyright (c) 2026 The llingr-logger-zap Authors
// SPDX-License-Identifier: Apache-2.0

package zaplogger_test

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TestStructuredFieldsForwarded confirms the variadic args are treated as
// key/value pairs (slog / zap SugaredLogger semantics). Note this deliberately
// differs from nexus.DefaultLogger, which printf-formats its args.
func TestStructuredFieldsForwarded(t *testing.T) {
	log, logs := newObserved(zapcore.DebugLevel)

	log.Info(context.Background(), "processed", "key", "b7K2mX9qRf4WpA", "partition", 3)

	e := logs.All()[0]
	if e.Message != "processed" {
		t.Errorf("message = %q, want %q", e.Message, "processed")
	}
	if e.Level != zapcore.InfoLevel {
		t.Errorf("level = %v, want info", e.Level)
	}
	fields := e.ContextMap()
	if fields["key"] != "b7K2mX9qRf4WpA" {
		t.Errorf("field key = %v, want b7K2mX9qRf4WpA", fields["key"])
	}
	if fields["partition"] != int64(3) {
		t.Errorf("field partition = %v (%T), want 3", fields["partition"], fields["partition"])
	}
}

// TestNativeZapFieldsTypedPath confirms callers can pass strongly-typed zap
// fields straight through (the reason for wrapping the non-sugared logger).
func TestNativeZapFieldsTypedPath(t *testing.T) {
	log, logs := newObserved(zapcore.DebugLevel)

	log.Info(context.Background(), "typed", zap.String("key", "abc"), zap.Int("partition", 7))

	fields := logs.All()[0].ContextMap()
	if fields["key"] != "abc" {
		t.Errorf("field key = %v, want abc", fields["key"])
	}
	if fields["partition"] != int64(7) {
		t.Errorf("field partition = %v (%T), want 7", fields["partition"], fields["partition"])
	}
}

// TestNonStringKeyMidListAdvancesByOne covers a non-string in a key position that
// is NOT the final argument: it must land under !BADKEY, and the key/value pair
// that follows must still parse correctly. That proves the scan advanced by
// exactly one past the bad key. Without this case, the advance in that branch is
// untested (a gap mutation testing surfaced: a flipped increment there goes
// undetected by coverage alone).
func TestNonStringKeyMidListAdvancesByOne(t *testing.T) {
	log, logs := newObserved(zapcore.DebugLevel)

	log.Info(context.Background(), "mixed", 42, "topic", "payments")

	fields := logs.All()[0].ContextMap()
	if fields["!BADKEY"] != int64(42) {
		t.Errorf("field !BADKEY = %v (%T), want 42", fields["!BADKEY"], fields["!BADKEY"])
	}
	if fields["topic"] != "payments" {
		t.Errorf("field topic = %v, want payments (scan did not advance past the bad key correctly)", fields["topic"])
	}
}

// TestBareErrorGetsErrorKey confirms a bare error in key position becomes a
// standard "error" field (zap.Error), matching zap's SugaredLogger, rather than
// landing under !BADKEY. This is the `log.Error(ctx, msg, err)` call shape.
func TestBareErrorGetsErrorKey(t *testing.T) {
	log, logs := newObserved(zapcore.DebugLevel)

	log.Error(context.Background(), "drain failed", errors.New("broker gone"))

	fields := logs.All()[0].ContextMap()
	if fields["error"] != "broker gone" {
		t.Errorf(`field error = %v, want "broker gone" (bare error not given the error key)`, fields["error"])
	}
	if _, present := fields["!BADKEY"]; present {
		t.Error("bare error landed under !BADKEY, want the standard error key")
	}
}

// TestBareErrorMidListAdvancesByOne confirms a bare error that is NOT the final
// argument consumes exactly one slot: the key/value pair after it must still
// parse correctly.
func TestBareErrorMidListAdvancesByOne(t *testing.T) {
	log, logs := newObserved(zapcore.DebugLevel)

	log.Error(context.Background(), "drain failed", errors.New("broker gone"), "topic", "payments")

	fields := logs.All()[0].ContextMap()
	if fields["error"] != "broker gone" {
		t.Errorf(`field error = %v, want "broker gone"`, fields["error"])
	}
	if fields["topic"] != "payments" {
		t.Errorf("field topic = %v, want payments (scan did not advance past the error correctly)", fields["topic"])
	}
}

// TestErrorInValuePositionStaysValue confirms the error special case only applies
// in key position: an error following a string key is that key's value.
func TestErrorInValuePositionStaysValue(t *testing.T) {
	log, logs := newObserved(zapcore.DebugLevel)

	log.Error(context.Background(), "drain failed", "cause", errors.New("broker gone"))

	fields := logs.All()[0].ContextMap()
	if fields["cause"] != "broker gone" {
		t.Errorf(`field cause = %v, want "broker gone"`, fields["cause"])
	}
	if _, present := fields["error"]; present {
		t.Error("error in value position was hoisted to its own error field")
	}
}

// TestMixedFieldsAndBadKey covers a native field mixed with a loose pair, plus a
// dangling value that must land under !BADKEY (matching slog/zap semantics).
func TestMixedFieldsAndBadKey(t *testing.T) {
	log, logs := newObserved(zapcore.DebugLevel)

	log.Error(context.Background(), "drain failed", zap.Int("attempt", 2), "topic", "payments", "dangling")

	fields := logs.All()[0].ContextMap()
	if fields["attempt"] != int64(2) {
		t.Errorf("field attempt = %v, want 2", fields["attempt"])
	}
	if fields["topic"] != "payments" {
		t.Errorf("field topic = %v, want payments", fields["topic"])
	}
	if fields["!BADKEY"] != "dangling" {
		t.Errorf("field !BADKEY = %v, want dangling", fields["!BADKEY"])
	}
}
