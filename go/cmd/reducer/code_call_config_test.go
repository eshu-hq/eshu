// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestLoadCodeCallProjectionConfigReadsAcceptanceScanLimit(t *testing.T) {
	t.Parallel()

	cfg := loadCodeCallProjectionConfig(func(k string) string {
		switch k {
		case codeCallProjectionBatchLimitEnv:
			return "250"
		case codeCallProjectionAcceptanceScanLimitEnv:
			return "20000"
		case codeCallProjectionPartitionCountEnv:
			return "16"
		case codeCallProjectionWorkersEnv:
			return "4"
		default:
			return ""
		}
	})

	if got, want := cfg.BatchLimit, 250; got != want {
		t.Fatalf("BatchLimit = %d, want %d", got, want)
	}
	if got, want := cfg.AcceptanceScanLimit, 20_000; got != want {
		t.Fatalf("AcceptanceScanLimit = %d, want %d", got, want)
	}
	if got, want := cfg.PartitionCount, 16; got != want {
		t.Fatalf("PartitionCount = %d, want %d", got, want)
	}
	if got, want := cfg.Workers, 4; got != want {
		t.Fatalf("Workers = %d, want %d", got, want)
	}
}

func TestLoadCodeCallProjectionConfigDefaultsAcceptanceScanLimit(t *testing.T) {
	t.Parallel()

	cfg := loadCodeCallProjectionConfig(func(string) string { return "" })

	if got, want := cfg.AcceptanceScanLimit, defaultCodeCallProjectionAcceptanceScanLimit; got != want {
		t.Fatalf("AcceptanceScanLimit = %d, want %d", got, want)
	}
	if got, want := cfg.PartitionCount, 8; got != want {
		t.Fatalf("PartitionCount = %d, want %d", got, want)
	}
	if got, want := cfg.Workers, 4; got != want {
		t.Fatalf("Workers = %d, want %d", got, want)
	}
}

func TestLoadCodeCallProjectionConfigUsesStableProcessLeaseOwner(t *testing.T) {
	t.Parallel()

	getenv := func(name string) string {
		if name == codeCallProjectionLeaseOwnerEnv {
			return "operator-code-call"
		}
		return ""
	}
	first := loadCodeCallProjectionConfig(getenv).LeaseOwner
	second := loadCodeCallProjectionConfig(getenv).LeaseOwner
	if first != second {
		t.Fatalf("code-call projection lease owner changed within one process boot: %q != %q", first, second)
	}
	if first == "operator-code-call" || !strings.HasPrefix(first, "operator-code-call:") {
		t.Fatalf("code-call projection lease owner = %q, want operator prefix plus process identity", first)
	}
	parts := strings.Split(first, ":")
	if len(parts) < 4 || parts[len(parts)-2] != strconv.Itoa(os.Getpid()) {
		t.Fatalf("code-call projection lease owner = %q, want hostname, current pid, and boot nonce suffix", first)
	}
	if len(parts[len(parts)-1]) < 16 {
		t.Fatalf("code-call projection lease owner = %q, want boot-unique nonce", first)
	}
}

func TestLoadCodeCallEdgeWriterTuningDefaultsToMeasuredLargeRepoBatch(t *testing.T) {
	t.Parallel()

	batchSize, groupBatchSize := loadCodeCallEdgeWriterTuning(func(string) string { return "" })

	if got, want := batchSize, 1000; got != want {
		t.Fatalf("batchSize = %d, want %d", got, want)
	}
	if got, want := groupBatchSize, defaultCodeCallEdgeGroupBatchSize; got != want {
		t.Fatalf("groupBatchSize = %d, want %d", got, want)
	}
}
