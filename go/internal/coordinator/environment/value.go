// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package environment

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Int reads key as a base-10 integer. A missing or blank value yields
// fallback; an unparseable value is an error naming the key.
func Int(getenv func(string) string, key string, fallback int) (int, error) {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

// Bool reads key as a Go boolean literal. A missing or blank value yields
// fallback; an unparseable value is an error naming the key.
func Bool(getenv func(string) string, key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

// Duration reads key as a [time.ParseDuration] string. A missing or blank
// value yields fallback; an unparseable value is an error naming the key.
func Duration(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}
