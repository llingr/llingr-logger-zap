// SPDX-FileCopyrightText: Copyright (c) 2026 The llingr-logger-zap Authors
// SPDX-License-Identifier: Apache-2.0

package zaplogger_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/llingr/llingr-logger-zap/zaplogger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// ctxKey is a private context key for the extractor tests.
type ctxKey struct{}

// TestContextExtractorReturnsFields exercises a ContextExtractor directly: it is
// just a func, so calling it with a populated context must yield the fields, and
// with a bare context must yield none.
func TestContextExtractorReturnsFields(t *testing.T) {
	var extract zaplogger.ContextExtractor = func(ctx context.Context) []zap.Field {
		id, ok := ctx.Value(ctxKey{}).(string)
		if ok {
			return []zap.Field{zap.String("trace_id", id)}
		}
		return nil
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, "trace-abc")
	got := extract(ctx)
	if len(got) != 1 || got[0].Key != "trace_id" || got[0].String != "trace-abc" {
		t.Errorf("extract(populated) = %+v, want one trace_id=trace-abc field", got)
	}

	got = extract(context.Background())
	if got != nil {
		t.Errorf("extract(bare) = %+v, want nil (no correlation data present)", got)
	}
}

// TestContextExtractorFlattensFields confirms a configured extractor pulls
// fields from the call's context and inlines them flat into the entry.
func TestContextExtractorFlattensFields(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	log := zaplogger.New(zap.New(core, zap.AddCaller())).WithContextExtractor(func(ctx context.Context) []zap.Field {
		id, ok := ctx.Value(ctxKey{}).(string)
		if ok {
			return []zap.Field{zap.String("trace_id", id)}
		}
		return nil
	})

	ctx := context.WithValue(context.Background(), ctxKey{}, "trace-abc")
	log.Info(ctx, "handled", "key", "k1")

	fields := logs.All()[0].ContextMap()
	if fields["trace_id"] != "trace-abc" {
		t.Errorf("trace_id = %v, want trace-abc (extractor not inlined)", fields["trace_id"])
	}
	if fields["key"] != "k1" {
		t.Errorf("explicit field key = %v, want k1", fields["key"])
	}
}

// traceIDExtractor pulls the test trace ID out of a context, shared by the
// inheritance tests below.
func traceIDExtractor(ctx context.Context) []zap.Field {
	id, ok := ctx.Value(ctxKey{}).(string)
	if ok {
		return []zap.Field{zap.String("trace_id", id)}
	}
	return nil
}

// TestWithInheritsContextExtractor confirms the documented With behaviour: a
// child created with bound fields keeps the parent's ContextExtractor, so its
// lines carry both the bound field and the extracted context fields.
func TestWithInheritsContextExtractor(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	parent := zaplogger.New(zap.New(core)).WithContextExtractor(traceIDExtractor)
	child := parent.With("topic", "payments")

	ctx := context.WithValue(context.Background(), ctxKey{}, "trace-abc")
	child.Info(ctx, "inherited")

	fields := logs.All()[0].ContextMap()
	if fields["trace_id"] != "trace-abc" {
		t.Errorf("trace_id = %v, want trace-abc (With dropped the extractor)",
			fields["trace_id"])
	}
	if fields["topic"] != "payments" {
		t.Errorf("bound field topic = %v, want payments", fields["topic"])
	}
}

// TestWithContextExtractorNilDisables confirms the documented escape hatch:
// passing nil yields a child with extraction off, and leaves the parent's
// extractor untouched.
func TestWithContextExtractorNilDisables(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	parent := zaplogger.New(zap.New(core)).WithContextExtractor(traceIDExtractor)
	child := parent.WithContextExtractor(nil)

	ctx := context.WithValue(context.Background(), ctxKey{}, "trace-abc")
	child.Info(ctx, "extraction off")
	parent.Info(ctx, "extraction still on")

	entries := logs.All()
	if _, present := entries[0].ContextMap()["trace_id"]; present {
		t.Error("child logged trace_id, want extraction disabled by WithContextExtractor(nil)")
	}
	if entries[1].ContextMap()["trace_id"] != "trace-abc" {
		t.Error("parent lost its extractor after a child disabled extraction")
	}
}

// TestContextExtractorIsLazy proves the extractor does not run for an entry that
// is dropped by the level gate, and does run (once) on one that is encoded: the
// whole point of using zap.Inline. It uses a real JSON core because that runs
// field marshaling at Write time; zaptest/observer defers it to inspection.
func TestContextExtractorIsLazy(t *testing.T) {
	var buf bytes.Buffer
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&buf),
		zapcore.WarnLevel,
	)
	calls := 0
	log := zaplogger.New(zap.New(core)).WithContextExtractor(func(context.Context) []zap.Field {
		calls++
		return []zap.Field{zap.String("trace_id", "x")}
	})
	ctx := context.Background()

	log.Debug(ctx, "below threshold") // dropped: extractor must not run
	log.Info(ctx, "below threshold")  // dropped: extractor must not run
	if calls != 0 {
		t.Fatalf("extractor ran %d times for dropped lines, want 0", calls)
	}

	log.Error(ctx, "above threshold") // encoded: extractor runs once
	if calls != 1 {
		t.Errorf("extractor ran %d times, want 1", calls)
	}
	if !strings.Contains(buf.String(), `"trace_id":"x"`) {
		t.Errorf("encoded output missing inlined field: %s", buf.String())
	}
}
