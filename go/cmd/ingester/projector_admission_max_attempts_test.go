// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
)

// TestLoadIngesterProjectorAdmissionMaxAttempts pins the
// ESHU_REDUCER_ADMISSION_MAX_ATTEMPTS knob (#10671): unset keeps the
// queue default (signaled by zero, which admissionMaxAttempts maps to
// 48); an explicit value must be positive; zero, negative, and
// non-numeric values fail startup because there is no disable mode.
func TestLoadIngesterProjectorAdmissionMaxAttempts(t *testing.T) {
	t.Parallel()

	t.Run("unset keeps default", func(t *testing.T) {
		t.Parallel()
		got, err := loadIngesterProjectorAdmissionMaxAttempts(mapGetenv(nil))
		if err != nil {
			t.Fatalf("load error = %v, want nil", err)
		}
		if got != 0 {
			t.Fatalf("max attempts = %d, want 0 (queue default)", got)
		}
	})

	t.Run("positive value passes through", func(t *testing.T) {
		t.Parallel()
		got, err := loadIngesterProjectorAdmissionMaxAttempts(mapGetenv(map[string]string{
			ingesterProjectorAdmissionMaxAttemptsEnv: "12",
		}))
		if err != nil {
			t.Fatalf("load error = %v, want nil", err)
		}
		if got != 12 {
			t.Fatalf("max attempts = %d, want 12", got)
		}
	})

	for _, raw := range []string{"0", "-1", "many", "4.5"} {
		t.Run("rejects "+raw, func(t *testing.T) {
			t.Parallel()
			if _, err := loadIngesterProjectorAdmissionMaxAttempts(mapGetenv(map[string]string{
				ingesterProjectorAdmissionMaxAttemptsEnv: raw,
			})); err == nil {
				t.Fatalf("load(%q) error = nil, want startup failure", raw)
			}
		})
	}

	t.Run("nil getenv keeps default", func(t *testing.T) {
		t.Parallel()
		got, err := loadIngesterProjectorAdmissionMaxAttempts(nil)
		if err != nil {
			t.Fatalf("load error = %v, want nil", err)
		}
		if got != 0 {
			t.Fatalf("max attempts = %d, want 0 (queue default)", got)
		}
	})
}
