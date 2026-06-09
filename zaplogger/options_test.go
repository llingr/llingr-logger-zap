// SPDX-FileCopyrightText: Copyright (c) 2026 The llingr-logger-zap Authors
// SPDX-License-Identifier: Apache-2.0

package zaplogger_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/llingr/llingr-logger-zap/zaplogger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// syncWriter is a zapcore.WriteSyncer whose Sync returns a fixed error, used to
// drive the ENOTTY-suppression tests without touching a real console.
type syncWriter struct{ syncErr error }

func (syncWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w syncWriter) Sync() error               { return w.syncErr }

// newWithSyncErr builds a Logger over a core whose Sync returns syncErr, applying
// the given options to the wrapper.
func newWithSyncErr(syncErr error, opts ...zaplogger.Option) *zaplogger.Logger {
	sink := syncWriter{
		syncErr: syncErr,
	}
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(sink),
		zapcore.DebugLevel,
	)
	return zaplogger.New(zap.New(core), opts...)
}

// newTeeWithSyncErrs builds a Logger over a tee of two cores whose sinks return
// the given Sync errors, mirroring the common console + file production setup.
// zap combines the per-core errors into one multi-error.
func newTeeWithSyncErrs(errA, errB error) *zaplogger.Logger {
	enc := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	core := zapcore.NewTee(
		zapcore.NewCore(enc, zapcore.AddSync(syncWriter{syncErr: errA}), zapcore.DebugLevel),
		zapcore.NewCore(enc, zapcore.AddSync(syncWriter{syncErr: errB}), zapcore.DebugLevel),
	)
	return zaplogger.New(zap.New(core))
}

// enottyErr returns the error shape a real console sink produces: an
// *os.PathError wrapping syscall.ENOTTY.
func enottyErr(path string) error {
	return &os.PathError{
		Op:   "sync",
		Path: path,
		Err:  syscall.ENOTTY,
	}
}

// TestPreserveHostCallerRespectsCallerOff confirms PreserveHostCaller leaves a
// host that did NOT enable caller capture with caller still off: the adapter does
// not force it on. This is the escape hatch for hosts that disabled caller on
// purpose (cost or privacy).
func TestPreserveHostCallerRespectsCallerOff(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	log := zaplogger.New(zap.New(core), zaplogger.PreserveHostCaller()) // host: no AddCaller

	log.Info(context.Background(), "caller stays off")

	c := logs.All()[0].Caller
	if c.Defined {
		t.Errorf("caller = %s:%d, want undefined (PreserveHostCaller forced it on)", c.File, c.Line)
	}
}

// TestPreserveHostCallerKeepsSkipWhenHostEnabled confirms that when the host DID
// enable caller capture, PreserveHostCaller still applies the wrapper skip, so the
// recorded site is the application's call site, not this package's frame.
func TestPreserveHostCallerKeepsSkipWhenHostEnabled(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	log := zaplogger.New(zap.New(core, zap.AddCaller()), zaplogger.PreserveHostCaller())

	_, _, line, _ := runtime.Caller(0)
	log.Info(context.Background(), "host enabled caller") // must stay directly below
	wantLine := line + 1

	c := logs.All()[0].Caller
	if !c.Defined {
		t.Fatal("caller not captured though host enabled it")
	}
	if filepath.Base(c.File) != "options_test.go" || c.Line != wantLine {
		t.Errorf("caller = %s:%d, want options_test.go:%d (wrapper frame not skipped)", c.File, c.Line, wantLine)
	}
}

// TestSyncSuppressesENOTTYByDefault confirms the default Sync swallows the ENOTTY
// error a console sink returns, so a clean shutdown reports no spurious failure.
func TestSyncSuppressesENOTTYByDefault(t *testing.T) {
	log := newWithSyncErr(enottyErr("/dev/stderr"))

	err := log.Sync()
	if err != nil {
		t.Errorf("Sync() = %v, want nil (ENOTTY should be suppressed by default)", err)
	}
}

// TestSyncPassthroughENOTTYReturnsRaw confirms the opt-out returns the raw ENOTTY.
func TestSyncPassthroughENOTTYReturnsRaw(t *testing.T) {
	log := newWithSyncErr(enottyErr("/dev/stderr"), zaplogger.SyncPassthroughENOTTY())

	err := log.Sync()
	if !errors.Is(err, syscall.ENOTTY) {
		t.Errorf("Sync() = %v, want the raw ENOTTY error", err)
	}
}

// TestSyncPassesThroughRealErrors confirms suppression is narrow: a non-ENOTTY
// Sync error is always returned, even by default, so genuine flush failures are
// never masked.
func TestSyncPassesThroughRealErrors(t *testing.T) {
	boom := errors.New("disk on fire")
	log := newWithSyncErr(boom)

	err := log.Sync()
	if !errors.Is(err, boom) {
		t.Errorf("Sync() = %v, want the real error passed through", err)
	}
}

// TestSyncTeeRealErrorNotMaskedByENOTTY is the multi-sink guarantee: with a tee
// of a console (ENOTTY) and a file sink whose fsync genuinely fails, the combined
// error must be returned. A bare errors.Is(err, ENOTTY) would match the console
// branch and swallow the file failure with it.
func TestSyncTeeRealErrorNotMaskedByENOTTY(t *testing.T) {
	boom := errors.New("disk on fire")
	log := newTeeWithSyncErrs(enottyErr("/dev/stderr"), boom)

	err := log.Sync()
	if err == nil {
		t.Fatal("Sync() = nil, the console ENOTTY masked a real flush failure")
	}
	if !errors.Is(err, boom) {
		t.Errorf("Sync() = %v, want it to carry the real error", err)
	}
}

// TestSyncTeeAllENOTTYSuppressed confirms suppression still works for multi-sink
// loggers when every sink reports ENOTTY (a tee of stdout and stderr, say).
func TestSyncTeeAllENOTTYSuppressed(t *testing.T) {
	log := newTeeWithSyncErrs(enottyErr("/dev/stdout"), enottyErr("/dev/stderr"))

	err := log.Sync()
	if err != nil {
		t.Errorf("Sync() = %v, want nil (every sink reported ENOTTY)", err)
	}
}

// childlessMulti is an error claiming the multi-error shape with no constituents.
// Nothing real produces this; it pins down the conservative choice that such an
// error is not treated as ENOTTY and passes through.
type childlessMulti struct{}

func (childlessMulti) Error() string   { return "childless multi-error" }
func (childlessMulti) Unwrap() []error { return nil }

// TestSyncEmptyMultiErrorPassesThrough confirms an empty multi-error is not
// vacuously "all ENOTTY": it is returned, not suppressed.
func TestSyncEmptyMultiErrorPassesThrough(t *testing.T) {
	log := newWithSyncErr(childlessMulti{})

	err := log.Sync()
	if err == nil {
		t.Error("Sync() = nil, want the childless multi-error passed through")
	}
}
