// SPDX-FileCopyrightText: Copyright (c) 2026 The llingr-logger-zap Authors
// SPDX-License-Identifier: Apache-2.0

package zaplogger

// Option configures a Logger at construction time, passed to New or NewSugared.
// See PreserveHostCaller and SyncReturnConsoleErrors.
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

// SyncReturnConsoleErrors makes Sync return zap's error verbatim instead of
// suppressing the errors a console sink produces from fsync: ENOTTY on
// darwin/BSD, and EINVAL on Linux when the sink is /dev/stdout or /dev/stderr.
// Neither indicates a real flush failure, which is why Sync swallows them by
// default; pass this option if the host wants to make that call itself. See
// Sync.
func SyncReturnConsoleErrors() Option {
	return func(l *Logger) {
		l.returnConsoleErrors = true
	}
}
