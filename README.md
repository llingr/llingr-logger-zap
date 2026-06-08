# llingr-logger-zap

[![Go Reference](https://pkg.go.dev/badge/github.com/llingr/llingr-logger-zap.svg)](https://pkg.go.dev/github.com/llingr/llingr-logger-zap)
[![Go Report Card](https://goreportcard.com/badge/github.com/llingr/llingr-logger-zap)](https://goreportcard.com/report/github.com/llingr/llingr-logger-zap)
[![Tag](https://img.shields.io/github/v/tag/llingr/llingr-logger-zap)](https://github.com/llingr/llingr-logger-zap/tags)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/llingr/llingr-logger-zap)](go.mod)

A [`nexus.Logger`](https://github.com/llingr/llingr-nexus) implementation
backed by [Uber's zap](https://github.com/uber-go/zap).

`llingr-nexus` ships a zero-dependency `DefaultLogger` (slog-based) for
convenience. For production deployments the recommendation is to plug in an
established logging library; this module is one such binding (zap). The
interface is small, so a zerolog, logrus, or bespoke binding is equally valid;
this is just the zap one.

- **Module:** `github.com/llingr/llingr-logger-zap`
- **Go:** 1.24+
- **Licence:** Apache-2.0
- **Depends on:** `llingr-nexus`, `go.uber.org/zap` (no other third-party deps)

## Call-site transparency

The wrapper is **call-site transparent**: the file and line zap records belong
to the application code that called the log method, never to this package. Each
log method adds one stack frame between your code and zap's caller capture, so
every constructor enables caller capture and applies a matching caller-skip to
chop that frame off. You get the call site of the invocation, not the call site
of the logger.

You do **not** need to build the underlying logger with `zap.AddCaller()`; the
constructor turns caller capture on for you (and re-enabling it on the host
logger is harmless). The one thing outside this package's control is the
encoder: the caller field is only rendered if the encoder config has a
`CallerKey` (zap's `NewProduction` / `NewDevelopment` presets set one by
default).

If your host has made a deliberate caller decision the adapter should not
override (caller turned off for cost or privacy, say), pass
`zaplogger.PreserveHostCaller()`:

```go
log := zaplogger.New(z, zaplogger.PreserveHostCaller())
```

The wrapper frame is still skipped, so if the host has caller capture on you
still get the application's call site; if the host turned it off, it stays off.

## Sync and the console ENOTTY error

`Sync()` flushes buffered entries; call it before the process exits. zap's
`Sync` runs `fsync` on the sink, and when the sink is a console (`os.Stdout` /
`os.Stderr`) the kernel rejects that with `ENOTTY` ("inappropriate ioctl for
device"). That is not a real flush failure, just zap asking a character device
to do something it cannot, so by default this wrapper treats that one error as
success and a clean shutdown reports nothing.

Pass `zaplogger.SyncPassthroughENOTTY()` to get zap's error back verbatim:

```go
log := zaplogger.New(z, zaplogger.SyncPassthroughENOTTY())
```

Suppression is narrow and targets `ENOTTY` specifically (the console-sync error
on darwin/BSD). Genuine flush failures are always returned, and other platforms'
errno for the same situation (Linux can return `EINVAL`) is deliberately passed
through rather than guessed at, to avoid masking a real `EINVAL`.

## Usage

This package **wires an existing logger; it does not create one.** The host
application supplies its own zap logger, configured however it likes, and you
wrap it:

```go
import (
	"github.com/llingr/llingr-logger-zap/zaplogger"
	"go.uber.org/zap"
)

z := app.Logger()                 // the host's own *zap.Logger, their config
log := zaplogger.New(z)           // wrap it as a nexus.Logger

consumer, _ := builder.
	WithLogger(log).
	Build()
```

Already holding a `*zap.SugaredLogger`? Use `zaplogger.NewSugared(s)`; it
desugars back to the typed logger for you.

There are deliberately no `NewProduction`/`NewDevelopment` helpers: building a
logger is zap's job (`zap.NewProduction()` etc.), and forcing a configuration is
exactly what this package avoids.

## Arguments

The message is logged **verbatim**: this package never runs `fmt.Sprintf` on it,
so a literal `%` is safe. The variadic arguments become structured fields, in
either of two forms, which may be mixed:

```go
// Native zap fields: the typed, allocation-light path:
log.Info(ctx, "processed", zap.String("key", msg.Key), zap.Int("partition", msg.Partition))

// Loose key/value pairs: slog / SugaredLogger style:
log.Info(ctx, "processed", "key", msg.Key, "partition", msg.Partition)
```

Because native `zap.Field` values are accepted directly, the package wraps the
**non-sugared** `*zap.Logger`. There is no `SugaredLogger` in the call path, so
the typed fast path is preserved and the common zero-argument call (the dominant
pattern in the llingr engine, which pre-formats its messages) costs nothing
beyond zap's own work.

A trailing key with no value, or a non-string where a key is expected, is
recorded under `!BADKEY`, matching slog and zap's `SugaredLogger`.

The `context.Context` argument is accepted to satisfy the interface; this
package does not read from it.

`With` returns a child logger with fields bound to every subsequent line,
preserving call-site transparency:

```go
topicLog := log.With("topic", "payments")
topicLog.Warn(ctx, "retry scheduled", "attempt", 2)
```

## Context fields

`nexus.Logger` threads a `context.Context` into every call, and most
implementations discard it. Register a `ContextExtractor` to pull ambient
correlation data (trace IDs, request IDs, tenant, ...) out of that context and
have it added to every line:

```go
log = log.WithContextExtractor(func(ctx context.Context) []zap.Field {
	if id, ok := ctx.Value(traceKey).(string); ok {
		return []zap.Field{zap.String("trace_id", id)}
	}
	return nil
})

log.Info(ctx, "handled") // => {... "trace_id": "abc123"}
```

The extractor is wired through `zap.Inline`, so:

- **Lazy.** It runs only when an entry clears the level gate and is actually
  encoded. A suppressed `Debug` line costs no extraction.
- **Flat.** The fields land at the top level of the entry, not nested under a
  key.

Which keys to read is application-specific, which is why it is a plugged-in
function rather than built in. Pass `nil` to disable it on a child logger.

## API

| Constructor                                         | Use                             |
|-----------------------------------------------------|---------------------------------|
| `New(*zap.Logger, ...Option) *Logger`               | Wrap an existing zap logger     |
| `NewSugared(*zap.SugaredLogger, ...Option) *Logger` | Wrap an existing sugared logger |

| Option                    | Effect                                                                                   |
|---------------------------|------------------------------------------------------------------------------------------|
| `PreserveHostCaller()`    | Do not force `zap.AddCaller()`; keep the host's caller setting (wrapper skip still used) |
| `SyncPassthroughENOTTY()` | Return zap's raw `Sync` error instead of suppressing the console `ENOTTY`                |

Methods: `Error`, `Warn`, `Info`, `Debug` (the `nexus.Logger` interface), plus
`With(args ...any) *Logger`,
`WithContextExtractor(ContextExtractor) *Logger`, and `Sync() error`.
