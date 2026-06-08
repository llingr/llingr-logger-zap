// SPDX-FileCopyrightText: Copyright (c) 2026 The llingr-logger-zap Authors
// SPDX-License-Identifier: Apache-2.0

package zaplogger_test

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/llingr/llingr-logger-zap/zaplogger"
	"github.com/llingr/llingr-nexus/nexus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// newObserved returns a wrapped Logger plus the observer that captures what it
// emits, with caller capture enabled so we can assert on the recorded call site.
func newObserved(level zapcore.Level) (*zaplogger.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(level)
	return zaplogger.New(zap.New(core, zap.AddCaller())), logs
}

// TestCallerIsApplicationNotPackage is the headline guarantee: the recorded
// caller must point at this test file (the invocation site), never at
// zaplogger.go (the wrapper). The log call sits on the line immediately after
// runtime.Caller so the expected line number is exact; keep them adjacent.
func TestCallerIsApplicationNotPackage(t *testing.T) {
	log, logs := newObserved(zapcore.DebugLevel)

	_, _, line, _ := runtime.Caller(0)
	log.Info(context.Background(), "hello") // must stay directly below the line above
	wantLine := line + 1

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	c := entries[0].Caller
	if !c.Defined {
		t.Fatal("caller not captured: build the zap logger with zap.AddCaller()")
	}
	got := filepath.Base(c.File)
	if got != "zaplogger_test.go" {
		t.Errorf("caller file = %s, want zaplogger_test.go (wrapper frame not skipped)", got)
	}
	if strings.HasSuffix(filepath.ToSlash(c.File), "zaplogger/zaplogger.go") {
		t.Errorf("caller points at the logger package itself: %s", c.File)
	}
	if c.Line != wantLine {
		t.Errorf("caller line = %d, want %d", c.Line, wantLine)
	}
}

// TestMessageNeverReformatted proves the logger does not run fmt.Sprintf: a
// percent verb in the message survives verbatim even with a trailing arg.
func TestMessageNeverReformatted(t *testing.T) {
	log, logs := newObserved(zapcore.DebugLevel)

	log.Info(context.Background(), "progress 50% complete", "n", 1)

	got := logs.All()[0].Message
	if got != "progress 50% complete" {
		t.Errorf("message = %q, want it verbatim", got)
	}
}

// TestLevelFiltering checks each level routes correctly and sub-threshold lines
// are dropped by the core.
func TestLevelFiltering(t *testing.T) {
	log, logs := newObserved(zapcore.WarnLevel)
	ctx := context.Background()

	log.Debug(ctx, "debug") // dropped
	log.Info(ctx, "info")   // dropped
	log.Warn(ctx, "warn")   // kept
	log.Error(ctx, "error") // kept

	got := logs.All()
	if len(got) != 2 {
		t.Fatalf("want 2 entries above warn, got %d", len(got))
	}
	if got[0].Level != zapcore.WarnLevel || got[1].Level != zapcore.ErrorLevel {
		t.Errorf("levels = %v, %v; want warn, error", got[0].Level, got[1].Level)
	}
}

// TestWithBindsFieldsAndKeepsCaller verifies child loggers carry bound fields
// and still attribute the application call site.
func TestWithBindsFieldsAndKeepsCaller(t *testing.T) {
	log, logs := newObserved(zapcore.DebugLevel)

	child := log.With("topic", "payments")
	_, _, line, _ := runtime.Caller(0)
	child.Info(context.Background(), "bound") // must stay directly below the line above
	wantLine := line + 1

	e := logs.All()[0]
	if e.ContextMap()["topic"] != "payments" {
		t.Errorf("bound field topic = %v, want payments", e.ContextMap()["topic"])
	}
	if filepath.Base(e.Caller.File) != "zaplogger_test.go" || e.Caller.Line != wantLine {
		t.Errorf("child caller = %s:%d, want zaplogger_test.go:%d", e.Caller.File, e.Caller.Line, wantLine)
	}
}

// TestSatisfiesNexusLogger is a runtime companion to the compile-time assertion
// in the package, and confirms NewSugared also yields a valid Logger.
func TestSatisfiesNexusLogger(t *testing.T) {
	var _ nexus.Logger = zaplogger.New(zap.NewNop())
	var _ nexus.Logger = zaplogger.NewSugared(zap.NewNop().Sugar())
}

// TestCallerCapturedWithoutHostAddCaller is the proof that New owns its contract:
// the host logger is built WITHOUT zap.AddCaller(), yet the caller is still
// captured and still points at this test file (the call site), because New
// enables caller capture itself. This is the case the package used to require the
// host to handle by hand.
func TestCallerCapturedWithoutHostAddCaller(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	log := zaplogger.New(zap.New(core)) // note: no zap.AddCaller() on the host logger

	_, _, line, _ := runtime.Caller(0)
	log.Info(context.Background(), "no host AddCaller") // must stay directly below
	wantLine := line + 1

	c := logs.All()[0].Caller
	if !c.Defined {
		t.Fatal("caller not captured even though New should enable it")
	}
	got := filepath.Base(c.File)
	if got != "zaplogger_test.go" {
		t.Errorf("caller file = %s, want zaplogger_test.go", got)
	}
	if c.Line != wantLine {
		t.Errorf("caller line = %d, want %d", c.Line, wantLine)
	}
}

// TestCallerRenderedWithoutHostAddCaller goes one step further than the observer
// test: it uses a real JSON core (production encoder config, which sets CallerKey)
// and a host logger with no zap.AddCaller(), and asserts the encoded line actually
// carries a "caller" field pointing at this test file. Proves the field renders,
// not just that the frame is captured.
func TestCallerRenderedWithoutHostAddCaller(t *testing.T) {
	var buf bytes.Buffer
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&buf),
		zapcore.DebugLevel,
	)
	log := zaplogger.New(zap.New(core)) // no zap.AddCaller() on the host logger

	log.Info(context.Background(), "rendered")

	out := buf.String()
	if !strings.Contains(out, `"caller":"zaplogger/zaplogger_test.go:`) {
		t.Errorf("encoded output missing caller field for this test file: %s", out)
	}
	if strings.Contains(out, "zaplogger/zaplogger.go") {
		t.Errorf("caller points at the wrapper, not the call site: %s", out)
	}
}
