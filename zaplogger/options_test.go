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

// enottyErr returns the error shape a darwin/BSD console sink produces: an
// *os.PathError wrapping syscall.ENOTTY.
func enottyErr(path string) error {
	return &os.PathError{
		Op:   "sync",
		Path: path,
		Err:  syscall.ENOTTY,
	}
}

// einvalErr returns the error shape a Linux console or pipe sink produces: an
// *os.PathError wrapping syscall.EINVAL.
func einvalErr(path string) error {
	return &os.PathError{
		Op:   "sync",
		Path: path,
		Err:  syscall.EINVAL,
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

// TestPreserveHarmlessSyncErrorsReturnsRaw confirms the opt-out returns the raw
// sync error verbatim instead of swallowing it.
func TestPreserveHarmlessSyncErrorsReturnsRaw(t *testing.T) {
	log := newWithSyncErr(enottyErr("/dev/stderr"), zaplogger.PreserveHarmlessSyncErrors())

	err := log.Sync()
	if !errors.Is(err, syscall.ENOTTY) {
		t.Errorf("Sync() = %v, want the raw ENOTTY error", err)
	}
}

// TestSyncSuppressesConsoleEINVALByDefault covers the Linux console shape: fsync
// on /dev/stdout or /dev/stderr (a terminal, or a pipe in a container) returns
// EINVAL, which is no more a real flush failure than darwin's ENOTTY.
func TestSyncSuppressesConsoleEINVALByDefault(t *testing.T) {
	log := newWithSyncErr(einvalErr("/dev/stdout"))

	err := log.Sync()
	if err != nil {
		t.Errorf("Sync() = %v, want nil (console EINVAL should be suppressed)", err)
	}
}

// TestSyncSuppressesNonConsoleSyncEINVAL covers the family the old path allowlist
// missed: fsync on any descriptor that cannot be synchronized (here a /dev/fd
// alias, but equally a tty or FIFO) returns sync-EINVAL, which is harmless and
// suppressed whatever the path.
func TestSyncSuppressesNonConsoleSyncEINVAL(t *testing.T) {
	log := newWithSyncErr(einvalErr("/dev/fd/1"))

	err := log.Sync()
	if err != nil {
		t.Errorf("Sync() = %v, want nil (sync-EINVAL is harmless on any unsyncable sink)", err)
	}
}

// TestSyncRealFlushFailurePassesThrough confirms a genuine flush failure on a
// sync operation (EIO here, as a failing disk reports) is returned, not masked.
func TestSyncRealFlushFailurePassesThrough(t *testing.T) {
	log := newWithSyncErr(&os.PathError{
		Op:   "sync",
		Path: "/var/log/app.log",
		Err:  syscall.EIO,
	})

	err := log.Sync()
	if !errors.Is(err, syscall.EIO) {
		t.Errorf("Sync() = %v, want the EIO flush failure passed through", err)
	}
}

// TestSyncEINVALFromNonSyncOpPassesThrough confirms suppression is narrow on the
// operation: an EINVAL that reaches Sync from a non-sync operation (a buffered
// sink surfacing a deferred write error, say) is a real failure and is returned,
// even on a console path.
func TestSyncEINVALFromNonSyncOpPassesThrough(t *testing.T) {
	log := newWithSyncErr(&os.PathError{
		Op:   "write",
		Path: "/dev/stdout",
		Err:  syscall.EINVAL,
	})

	err := log.Sync()
	if !errors.Is(err, syscall.EINVAL) {
		t.Errorf("Sync() = %v, want the non-sync EINVAL passed through", err)
	}
}

// TestSyncBareEINVALPassesThrough confirms a bare EINVAL errno, which carries no
// operation to confirm it came from fsync, is not suppressed.
func TestSyncBareEINVALPassesThrough(t *testing.T) {
	log := newWithSyncErr(syscall.EINVAL)

	err := log.Sync()
	if !errors.Is(err, syscall.EINVAL) {
		t.Errorf("Sync() = %v, want the bare EINVAL passed through", err)
	}
}

// TestSyncPathErrorJoinedErrnoPassesThrough pins the node-local guarantee: a
// PathError whose Err pairs a harmless errno with a real error must not be
// suppressed. os.File.Sync never produces this shape (Err is a bare errno), so
// the direct == comparison both matches every real shape and refuses to see into
// the join the way errors.Is would, keeping the every-constituent promise honest.
func TestSyncPathErrorJoinedErrnoPassesThrough(t *testing.T) {
	boom := errors.New("disk on fire")
	log := newWithSyncErr(&os.PathError{
		Op:   "sync",
		Path: "/dev/stdout",
		Err:  errors.Join(syscall.EINVAL, boom),
	})

	err := log.Sync()
	if !errors.Is(err, boom) {
		t.Errorf("Sync() = %v, want the real error passed through, not swallowed via the joined EINVAL", err)
	}
}

// TestSyncSuppressesBareENOTTYErrno covers a sink that returns the errno without
// a PathError wrapper; it is still recognisably the console-sync error.
func TestSyncSuppressesBareENOTTYErrno(t *testing.T) {
	log := newWithSyncErr(syscall.ENOTTY)

	err := log.Sync()
	if err != nil {
		t.Errorf("Sync() = %v, want nil (bare ENOTTY errno should be suppressed)", err)
	}
}

// TestSyncPathErrorWithOtherErrnoPassesThrough confirms a PathError carrying an
// errno outside the console family (EBADF here) is returned even on a console
// path.
func TestSyncPathErrorWithOtherErrnoPassesThrough(t *testing.T) {
	log := newWithSyncErr(&os.PathError{
		Op:   "sync",
		Path: "/dev/stderr",
		Err:  syscall.EBADF,
	})

	err := log.Sync()
	if !errors.Is(err, syscall.EBADF) {
		t.Errorf("Sync() = %v, want the EBADF passed through", err)
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
