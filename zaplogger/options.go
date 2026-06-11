// SPDX-FileCopyrightText: Copyright (c) 2026 The llingr-logger-zap Authors
// SPDX-License-Identifier: Apache-2.0

package zaplogger

// Option configures a Logger at construction time, passed to New or NewSugared.
// See PreserveHostCaller and PreserveHarmlessSyncErrors.
type Option func(*Logger)

// PreserveHostCaller leaves the host logger's caller configuration exactly as the
// host built it: the constructor will not call zap.AddCaller(). The wrapper frame
// is still skipped (via zap.AddCallerSkip), so if the host has caller capture on
// the recorded site is still the application's, and if the host turned it off (for
// cost or privacy) it stays off. Use this when the host has made a deliberate
// caller decision the adapter should not override.
func PreserveHostCaller() Option {
	return func(l *Logger) {
		l.preserveHostCaller = true
	}
}

// PreserveHarmlessSyncErrors makes Sync return zap's error verbatim instead of
// swallowing the harmless ones it discards by default.
//
// Some sinks cannot be fsync'd: a console, pipe, FIFO, tty, or socket. Calling
// fsync on them fails, but nothing was lost, so by default Sync treats that
// family as success (see Sync). Pass this option to receive those errors
// unfiltered, for a host that would rather inspect or report them itself, or that
// does not want the adapter making the call for it.
//
// The family, for those who want the detail: ENOTTY on darwin/BSD, and EINVAL on
// a sync operation on Linux (per fsync(2), EINVAL means the descriptor type does
// not support synchronization). Genuine flush failures (EIO, ENOSPC, EDQUOT) are
// returned with or without this option.
func PreserveHarmlessSyncErrors() Option {
	return func(l *Logger) {
		l.preserveHarmlessSyncErrors = true
	}
}
