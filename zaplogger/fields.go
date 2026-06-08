// SPDX-FileCopyrightText: Copyright (c) 2026 The llingr-logger-zap Authors
// SPDX-License-Identifier: Apache-2.0

package zaplogger

import "go.uber.org/zap"

// badKey labels a loose argument that could not be paired into a key/value, in
// line with slog and zap's SugaredLogger.
const badKey = "!BADKEY"

// toFields converts the nexus.Logger variadic into zap fields. It accepts native
// zap.Field values (the typed path), loose key/value pairs (slog style), and any
// mix of the two. A trailing key with no value, or a non-string where a key is
// expected, is recorded under "!BADKEY", matching slog and zap's SugaredLogger.
// An empty argument list returns nil, so the common no-field call is free.
func toFields(args []any) []zap.Field {
	if len(args) == 0 {
		return nil
	}
	fields := make([]zap.Field, 0, len(args))
	for i := 0; i < len(args); {
		f, ok := args[i].(zap.Field)
		if ok {
			fields = append(fields, f)
			i++
			continue
		}
		if i == len(args)-1 {
			fields = append(fields, zap.Any(badKey, args[i]))
			break
		}
		key, ok := args[i].(string)
		if !ok {
			fields = append(fields, zap.Any(badKey, args[i]))
			i++
			continue
		}
		fields = append(fields, zap.Any(key, args[i+1]))
		i += 2
	}
	return fields
}
