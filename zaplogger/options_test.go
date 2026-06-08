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
	enotty := &os.PathError{
		Op:   "sync",
		Path: "/dev/stderr",
		Err:  syscall.ENOTTY,
	}
	log := newWithSyncErr(enotty)

	err := log.Sync()
	if err != nil {
		t.Errorf("Sync() = %v, want nil (ENOTTY should be suppressed by default)", err)
	}
}

// TestSyncPassthroughENOTTYReturnsRaw confirms the opt-out returns the raw ENOTTY.
func TestSyncPassthroughENOTTYReturnsRaw(t *testing.T) {
	enotty := &os.PathError{
		Op:   "sync",
		Path: "/dev/stderr",
		Err:  syscall.ENOTTY,
	}
	log := newWithSyncErr(enotty, zaplogger.SyncPassthroughENOTTY())

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
