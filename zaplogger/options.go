// SPDX-FileCopyrightText: Copyright (c) 2026 The llingr-logger-zap Authors
// SPDX-License-Identifier: Apache-2.0

package zaplogger

// Option configures a Logger at construction time, passed to New or NewSugared.
// See PreserveHostCaller and SyncPassthroughENOTTY.
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

// SyncPassthroughENOTTY makes Sync return zap's error verbatim, including the
// ENOTTY ("inappropriate ioctl for device") error that a console sink
// (stdout/stderr) returns from its underlying fsync. Without it, Sync treats that
// specific error as success, since it is not a real flush failure. See Sync.
func SyncPassthroughENOTTY() Option {
	return func(l *Logger) {
		l.syncRawENOTTY = true
	}
}
