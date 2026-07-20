// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"math"
)

func finiteGraphFloat(row map[string]any, key, subject string) (float64, error) {
	value := floatVal(row, key)
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%s %s must be finite", subject, key)
	}
	return value, nil
}
