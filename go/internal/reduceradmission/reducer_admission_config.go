// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"fmt"
	"strconv"
	"strings"
)

// loadReducerAdmissionRetryingMarks parses the graph-write-pressure high/low
// marks and validates the hysteresis invariant (low < high when the gate is
// enabled). A low mark of zero is clamped to the default fraction of the high
// mark so an operator who only sets the high mark still gets sane hysteresis.
func loadReducerAdmissionRetryingMarks(getenv func(string) string, config *reducerAdmissionConfig) error {
	rawHigh := strings.TrimSpace(getenv(reducerAdmissionRetryingHighWaterMarkEnv))
	if rawHigh != "" {
		high, err := strconv.ParseInt(rawHigh, 10, 64)
		if err != nil {
			return fmt.Errorf("parse %s: %w", reducerAdmissionRetryingHighWaterMarkEnv, err)
		}
		if high < 0 {
			return fmt.Errorf("%s must be zero or greater", reducerAdmissionRetryingHighWaterMarkEnv)
		}
		config.RetryingHighWaterMark = high
	}

	rawLow := strings.TrimSpace(getenv(reducerAdmissionRetryingLowWaterMarkEnv))
	if rawLow != "" {
		low, err := strconv.ParseInt(rawLow, 10, 64)
		if err != nil {
			return fmt.Errorf("parse %s: %w", reducerAdmissionRetryingLowWaterMarkEnv, err)
		}
		if low < 0 {
			return fmt.Errorf("%s must be zero or greater", reducerAdmissionRetryingLowWaterMarkEnv)
		}
		config.RetryingLowWaterMark = low
	}

	if !config.graphWritePressureEnabled() {
		return nil
	}
	if config.RetryingLowWaterMark <= 0 || config.RetryingLowWaterMark > config.RetryingHighWaterMark {
		// Default or out-of-range low mark: clamp below the high mark so the
		// hysteresis gap is always valid. An explicit low mark above the high
		// mark is an operator error.
		if rawLow != "" && config.RetryingLowWaterMark > config.RetryingHighWaterMark {
			return fmt.Errorf("%s (%d) must be less than %s (%d)",
				reducerAdmissionRetryingLowWaterMarkEnv, config.RetryingLowWaterMark,
				reducerAdmissionRetryingHighWaterMarkEnv, config.RetryingHighWaterMark)
		}
		config.RetryingLowWaterMark = clampRetryingLowWaterMark(config.RetryingHighWaterMark)
	}
	return nil
}

// clampRetryingLowWaterMark derives a default low-water mark from the high-water
// mark: one fifth of the high mark, with a floor of 1 so the gate can always
// release.
func clampRetryingLowWaterMark(high int64) int64 {
	low := high / 5
	if low < 1 {
		low = 1
	}
	return low
}
