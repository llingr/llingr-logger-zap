// SPDX-FileCopyrightText: Copyright (c) 2026 The llingr-logger-zap Authors
// SPDX-License-Identifier: Apache-2.0

// Package zaplogger wires an existing go.uber.org/zap logger into a nexus.Logger.
// It does not create loggers: the host supplies its own (sugared or not) via New
// or NewSugared.
//
//   - Arguments: the message is logged verbatim (never fmt.Sprintf'd); the
//     variadic args become structured fields, as native zap.Field values or
//     loose key/value pairs, mixed freely. A bare error becomes an "error"
//     field, as in zap's SugaredLogger. See toFields.
//   - Call site: the constructors enable caller capture and skip this package's
//     own frame, so zap records the application's file and line, not this
//     package's. No zap.AddCaller() on the host logger is required. Pass
//     PreserveHostCaller to leave the host's caller setting untouched instead.
//   - Context: by default the context is ignored. WithContextExtractor lazily
//     adds fields pulled from it (trace IDs and the like) on every line.
//   - Sync: by default Sync swallows the errors a console sink returns from
//     fsync (ENOTTY on darwin/BSD; EINVAL on the standard console paths on
//     Linux), since they are not real flush failures. Pass
//     SyncReturnConsoleErrors to get the raw error back. See Sync.
package zaplogger

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"syscall"

	"github.com/llingr/llingr-nexus/nexus"
	"go.uber.org/zap"
)

// callerSkip accounts for this package's wrapper method (Info/Warn/Error/Debug)
// sitting between the application's call site and zap's caller capture. It is
// applied in every constructor so the recorded caller is the application's, not
// ours. Each method calls the matching *zap.Logger level method directly, so
// exactly one frame needs skipping.
const callerSkip = 1

// Logger adapts a *zap.Logger to nexus.Logger.
type Logger struct {
	z                   *zap.Logger
	extract             ContextExtractor
	preserveHostCaller  bool // construction-only: skip zap.AddCaller(), keep host's setting
	returnConsoleErrors bool // when true, Sync returns console fsync errors verbatim
}

// compile-time assertion that Logger satisfies the nexus contract.
var _ nexus.Logger = (*Logger)(nil)

// New wraps an existing *zap.Logger as a nexus.Logger. By default caller capture is
// enabled automatically and this package's own stack frame is skipped, so the
// recorded caller is the application's call site, not this wrapper. The host does
// not need to build z with zap.AddCaller(); doing so is harmless (both options
// compose). Pass PreserveHostCaller to leave the host's caller setting untouched.
//
// The caller field still only appears if the encoder is configured to emit it
// (a CallerKey in the encoder config, as zap's production/development presets set
// by default). New cannot control the encoder, only the logger.
//
// New panics if z is nil: this package wires an existing logger.
func New(z *zap.Logger, opts ...Option) *Logger {
	if z == nil {
		panic("zaplogger: New requires a non-nil *zap.Logger")
	}
	l := &Logger{}
	for _, opt := range opts {
		opt(l)
	}
	zopts := []zap.Option{zap.AddCallerSkip(callerSkip)}
	if !l.preserveHostCaller {
		zopts = append(zopts, zap.AddCaller())
	}
	l.z = z.WithOptions(zopts...)
	return l
}

// NewSugared wraps an existing *zap.SugaredLogger. It desugars to the underlying
// *zap.Logger, so the typed field path is restored and call-site transparency is
// preserved. Options are applied as in New. Like New, it panics if s is nil.
func NewSugared(s *zap.SugaredLogger, opts ...Option) *Logger {
	if s == nil {
		panic("zaplogger: NewSugared requires a non-nil *zap.SugaredLogger")
	}
	return New(s.Desugar(), opts...)
}

// With returns a child Logger with the given fields bound to every subsequent
// log line. The arguments follow the same rules as the log methods (native zap
// fields or loose key/value pairs). Any ContextExtractor is inherited.
// Call-site transparency is preserved.
func (l *Logger) With(args ...any) *Logger {
	fields := toFields(args)
	return &Logger{
		z:                   l.z.With(fields...),
		extract:             l.extract,
		returnConsoleErrors: l.returnConsoleErrors,
	}
}

// WithContextExtractor returns a child Logger that, for every subsequent call,
// lazily adds the fields the extractor pulls from the call's context.Context.
// Pass nil to disable extraction on the child. See ContextExtractor.
func (l *Logger) WithContextExtractor(extract ContextExtractor) *Logger {
	return &Logger{
		z:                   l.z,
		extract:             extract,
		returnConsoleErrors: l.returnConsoleErrors,
	}
}

// Sync flushes any buffered log entries. Call it before the process exits.
//
// zap's Sync calls fsync on the underlying sink, and a console sink cannot
// fsync: the kernel rejects the call. This is not a real flush failure, so by
// default Sync treats that family of errors as success and a clean shutdown
// reports nothing. Two shapes are recognised: ENOTTY ("inappropriate ioctl for
// device", the darwin/BSD console error), and EINVAL only when the failing path
// is /dev/stdout or /dev/stderr (the Linux error for a console, or a pipe on
// the standard streams, as in containers). An EINVAL from any other path is a
// genuine error and is returned. Construct with SyncReturnConsoleErrors to
// receive the raw error instead.
//
// With a multi-sink logger (zapcore.NewTee) zap combines the per-core Sync
// errors into one multi-error. Suppression requires that EVERY constituent is a
// console-sync error: if a console reports ENOTTY and a file sink reports a
// real fsync failure in the same Sync, the combined error is returned, not
// swallowed.
func (l *Logger) Sync() error {
	err := l.z.Sync()
	if err == nil || l.returnConsoleErrors {
		return err
	}
	if allConsoleSyncErrors(err) {
		// console sink can't fsync; not a real failure, so swallow it by default
		return nil
	}
	return err
}

// allConsoleSyncErrors reports whether every branch of err's wrapping tree
// bottoms out in a harmless console-sync error (see consoleSyncError).
func allConsoleSyncErrors(err error) bool {
	for err != nil {
		if multi, ok := err.(interface{ Unwrap() []error }); ok {
			errs := multi.Unwrap()
			if len(errs) == 0 {
				// an empty multi-error is not a console error; pass it through
				return false
			}
			for _, e := range errs {
				if !allConsoleSyncErrors(e) {
					return false
				}
			}
			return true
		}
		if consoleSyncError(err) {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// consoleSyncError reports whether this single node is the error a console (or
// pipe) sink returns from fsync: ENOTTY from darwin/BSD, or EINVAL when the
// path is one of the standard console devices, as Linux reports for stdout and
// stderr. The path constraint keeps a genuine EINVAL from a real sink out of
// suppression. Node-local on purpose: errors.Is from here could traverse into a
// nested multi-error and match a single harmless branch; the walk over
// structure belongs to allConsoleSyncErrors.
func consoleSyncError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.ENOTTY
	}
	pathErr, ok := err.(*fs.PathError)
	if !ok {
		return false
	}
	if errors.Is(pathErr.Err, syscall.ENOTTY) {
		return true
	}
	if !errors.Is(pathErr.Err, syscall.EINVAL) {
		return false
	}
	return pathErr.Path == os.Stdout.Name() || pathErr.Path == os.Stderr.Name()
}

// Error logs at error level. See the package documentation for argument
// semantics. The context is read only when a ContextExtractor is configured.
func (l *Logger) Error(ctx context.Context, msg string, args ...any) {
	l.z.Error(msg, l.compose(ctx, args)...)
}

// Warn logs at warn level. See Error for argument semantics.
func (l *Logger) Warn(ctx context.Context, msg string, args ...any) {
	l.z.Warn(msg, l.compose(ctx, args)...)
}

// Info logs at info level. See Error for argument semantics.
func (l *Logger) Info(ctx context.Context, msg string, args ...any) {
	l.z.Info(msg, l.compose(ctx, args)...)
}

// Debug logs at debug level. See Error for argument semantics.
func (l *Logger) Debug(ctx context.Context, msg string, args ...any) {
	l.z.Debug(msg, l.compose(ctx, args)...)
}

// compose turns the call's variadic into zap fields and, when a ContextExtractor
// is configured, appends a lazily-marshaled context field. It runs as an
// argument expression to the level method, so it adds no stack frame and does
// not affect caller attribution.
func (l *Logger) compose(ctx context.Context, args []any) []zap.Field {
	fields := toFields(args)
	if l.extract != nil && ctx != nil {
		marshaler := ctxMarshaler{
			ctx:     ctx,
			extract: l.extract,
		}
		fields = append(fields, zap.Inline(marshaler))
	}
	return fields
}
