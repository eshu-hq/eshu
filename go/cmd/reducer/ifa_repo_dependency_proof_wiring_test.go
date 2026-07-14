// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package main

import (
	"strconv"
	"testing"
)

func TestLoadIfaRepoDependencyProofWorkers(t *testing.T) {
	t.Parallel()

	for _, workers := range []int{1, 2, 4} {
		workers := workers
		t.Run(strconv.Itoa(workers), func(t *testing.T) {
			t.Parallel()
			cfg := loadRepoDependencyProjectionConfig(func(name string) string {
				if name == ifaRepoDependencyProofWorkersEnv {
					return strconv.Itoa(workers)
				}
				return ""
			})
			if got := cfg.Workers; got != workers {
				t.Fatalf("repo dependency proof workers = %d, want %d", got, workers)
			}
		})
	}
}
