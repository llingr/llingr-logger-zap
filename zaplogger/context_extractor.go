// SPDX-FileCopyrightText: Copyright (c) 2026 The llingr-logger-zap Authors
// SPDX-License-Identifier: Apache-2.0

package zaplogger

import (
	"context"

	"go.uber.org/zap"
)

// ContextExtractor pulls structured fields out of a context.Context, for
// callers that stash ambient correlation data (trace IDs, request IDs, tenant,
// and so on) there. It is invoked lazily, only when an entry clears the level
// gate and is actually encoded, so extraction costs nothing for dropped lines.
// Returning nil or an empty slice is fine and adds no fields.
//
// Register one with Logger.WithContextExtractor.
type ContextExtractor func(ctx context.Context) []zap.Field
