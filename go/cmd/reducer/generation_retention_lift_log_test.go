// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// TestLogGenerationRetentionHardCeilingLiftOnlyWhenDerivedAboveDefault pins
// the #7611 startup signal: one INFO record when the unset hard ceiling was
// raised to a soft window longer than 2160h, and silence otherwise.
func TestLogGenerationRetentionHardCeilingLiftOnlyWhenDerivedAboveDefault(t *testing.T) {
	silent := []struct {
		name string
		env  map[string]string
	}{
		{name: "defaults"},
		{name: "explicit hard ceiling", env: map[string]string{
			generationRetentionMaxSupersededAgeEnv:     "87600h",
			generationRetentionHardMaxSupersededAgeEnv: "87600h",
		}},
		{name: "soft window at the default ceiling", env: map[string]string{
			generationRetentionMaxSupersededAgeEnv: "2160h",
		}},
		{name: "soft window below the default ceiling", env: map[string]string{
			generationRetentionMaxSupersededAgeEnv: "720h",
		}},
	}
	for _, tc := range silent {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			cfg := loadGenerationRetentionConfig(func(key string) string { return tc.env[key] })

			logGenerationRetentionHardCeilingLift(context.Background(), logger, cfg)
			if buf.Len() != 0 {
				t.Fatalf("want no record, got %s", buf.String())
			}
		})
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	cfg := loadGenerationRetentionConfig(func(key string) string {
		if key == generationRetentionMaxSupersededAgeEnv {
			return "87600h"
		}
		return ""
	})
	logGenerationRetentionHardCeilingLift(context.Background(), logger, cfg)
	out := buf.String()
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("want exactly one record, got %q", out)
	}
	for _, want := range []string{
		`"level":"INFO"`,
		`"hard_env":"ESHU_GENERATION_RETENTION_HARD_MAX_SUPERSEDED_AGE"`,
		`"soft_env":"ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE"`,
		`"effective_hard_max_superseded_age":"87600h0m0s"`,
		`"max_superseded_age":"87600h0m0s"`,
		`"default_hard_max_superseded_age":"2160h0m0s"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("record %q missing %s", out, want)
		}
	}

	// A nil logger must not panic.
	logGenerationRetentionHardCeilingLift(context.Background(), nil, cfg)
}
