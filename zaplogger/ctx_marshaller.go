// SPDX-FileCopyrightText: Copyright (c) 2026 The llingr-logger-zap Authors
// SPDX-License-Identifier: Apache-2.0

package zaplogger

import (
	"context"

	"go.uber.org/zap/zapcore"
)

// ctxMarshaler defers a ContextExtractor so the bound context's fields are
// produced at encode time and flattened into the entry via zap.Inline.
type ctxMarshaler struct {
	ctx     context.Context
	extract ContextExtractor
}

// MarshalLogObject runs the extractor at encode time and writes its fields
// straight into the entry encoder, so they flatten in rather than nesting.
func (m ctxMarshaler) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	for _, f := range m.extract(m.ctx) {
		f.AddTo(enc)
	}
	return nil
}
