// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAnnotatedScreenErrorKeepsPhaseAndCause(t *testing.T) {
	cause := errors.New("statement timeout")
	err := annotatedScreenError("candidate_term", "a%c", time.Now().Add(-2*time.Second), cause)
	if !errors.Is(err, cause) {
		t.Fatal("annotated error lost its cause")
	}
	for _, want := range []string{"candidate_term", `"a%c"`, "elapsed="} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}
