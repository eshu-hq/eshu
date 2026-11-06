// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// generationRetentionMaxSupersededAgeEnv is the soft cutoff the reducer prunes
// superseded generations by. The coordinator reads it only to warn when a Go
// reachability rotation outgrows it (#10089), so a deployment that overrides it
// must set it on both runtimes.
const generationRetentionMaxSupersededAgeEnv = "ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE"

// generationRetentionSoftCutoff returns the configured soft cutoff. An unset,
// unparsable, or non-positive value resolves to the policy default, as it does in
// the reducer, so both runtimes read one rule.
func generationRetentionSoftCutoff(getenv func(string) string) time.Duration {
	fallback := postgres.DefaultGenerationRetentionPolicy().MaxSupersededAge
	cutoff, err := time.ParseDuration(strings.TrimSpace(getenv(generationRetentionMaxSupersededAgeEnv)))
	if err != nil || cutoff <= 0 {
		return fallback
	}
	return cutoff
}
