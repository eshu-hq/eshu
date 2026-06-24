// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// envFunc builds a getenv closure backed by a fixed map.
func envFunc(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

// TestSecretsIAMGraphProjectionWriterDefaultsOff proves an unset flag keeps the
// writer nil, which keeps DomainSecretsIAMGraphProjection unregistered. This is
// the ADR #1314 §14 default: live graph writes never start without an explicit
// opt-in.
func TestSecretsIAMGraphProjectionWriterDefaultsOff(t *testing.T) {
	t.Parallel()

	writer, err := secretsIAMGraphProjectionWriter(envFunc(nil), nil, 100, nil)
	if err != nil {
		t.Fatalf("writer error = %v, want nil", err)
	}
	if writer != nil {
		t.Fatalf("writer = %v, want nil when flag is unset", writer)
	}
}

// TestSecretsIAMGraphProjectionWriterEnabled proves a truthy flag constructs a
// live writer so the additive registry gate registers the projection domain.
func TestSecretsIAMGraphProjectionWriterEnabled(t *testing.T) {
	t.Parallel()

	getenv := envFunc(map[string]string{secretsIAMGraphProjectionEnabledEnv: "true"})
	writer, err := secretsIAMGraphProjectionWriter(getenv, nil, 100, nil)
	if err != nil {
		t.Fatalf("writer error = %v, want nil", err)
	}
	if writer == nil {
		t.Fatal("writer = nil, want a live writer when flag is enabled")
	}
}

// TestSecretsIAMGraphProjectionWriterMalformed proves a non-boolean value is a
// hard error, never a silent default, so a typo cannot read as either state.
func TestSecretsIAMGraphProjectionWriterMalformed(t *testing.T) {
	t.Parallel()

	getenv := envFunc(map[string]string{secretsIAMGraphProjectionEnabledEnv: "yes-please"})
	if _, err := secretsIAMGraphProjectionWriter(getenv, nil, 100, nil); err == nil {
		t.Fatal("writer error = nil, want non-nil for malformed flag value")
	}
}
