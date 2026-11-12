// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/clock"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

func funnelGetenv(value string) func(string) string {
	return func(k string) string {
		if k == reducerBatchClaimFunnelEnabledEnv {
			return value
		}
		return ""
	}
}

func TestConfigureReducerQueueCarriesFunnelEnabled(t *testing.T) {
	t.Parallel()

	queue, err := configureReducerQueue(
		nil,
		runtimecfg.RetryPolicyConfig{},
		nil,
		false,
		funnelGetenv("true"),
		runtimecfg.GraphBackendNornicDB,
		clock.System(),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("configureReducerQueue: %v", err)
	}
	if !queue.FunnelTelemetryEnabled {
		t.Fatal("queue.FunnelTelemetryEnabled = false with the flag true: the env-to-queue hop is broken and the flag silently never enables")
	}
}

func TestConfigureReducerQueueFunnelDefaultsOff(t *testing.T) {
	t.Parallel()

	queue, err := configureReducerQueue(
		nil,
		runtimecfg.RetryPolicyConfig{},
		nil,
		false,
		funnelGetenv(""),
		runtimecfg.GraphBackendNornicDB,
		clock.System(),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("configureReducerQueue: %v", err)
	}
	if queue.FunnelTelemetryEnabled {
		t.Fatal("queue.FunnelTelemetryEnabled = true with the flag unset, want default off (#10565 ACCEPT-(b))")
	}
}

func TestConfigureReducerQueueRejectsBadFunnelValue(t *testing.T) {
	t.Parallel()

	_, err := configureReducerQueue(
		nil,
		runtimecfg.RetryPolicyConfig{},
		nil,
		false,
		funnelGetenv("yes-please"),
		runtimecfg.GraphBackendNornicDB,
		clock.System(),
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("configureReducerQueue error = nil with an unparsable flag, want startup error")
	}
	if !strings.Contains(err.Error(), reducerBatchClaimFunnelEnabledEnv) {
		t.Fatalf("error %q does not name %s", err, reducerBatchClaimFunnelEnabledEnv)
	}
}

func TestLoadReducerBatchClaimFunnelEnabledDefaultsOff(t *testing.T) {
	t.Parallel()

	got, err := loadReducerBatchClaimFunnelEnabled(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadReducerBatchClaimFunnelEnabled() error = %v", err)
	}
	if got {
		t.Fatal("loadReducerBatchClaimFunnelEnabled() = true, want false (default off until the QA-lane delta proof, #10565)")
	}
}

func TestLoadReducerBatchClaimFunnelEnabledReadsTrue(t *testing.T) {
	t.Parallel()

	got, err := loadReducerBatchClaimFunnelEnabled(func(k string) string {
		if k == reducerBatchClaimFunnelEnabledEnv {
			return "true"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("loadReducerBatchClaimFunnelEnabled() error = %v", err)
	}
	if !got {
		t.Fatal("loadReducerBatchClaimFunnelEnabled() = false, want true")
	}
}

func TestLoadReducerBatchClaimFunnelEnabledRejectsUnparsable(t *testing.T) {
	t.Parallel()

	_, err := loadReducerBatchClaimFunnelEnabled(func(k string) string {
		if k == reducerBatchClaimFunnelEnabledEnv {
			return "yes-please"
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadReducerBatchClaimFunnelEnabled() error = nil, want startup error")
	}
	if !strings.Contains(err.Error(), reducerBatchClaimFunnelEnabledEnv) {
		t.Fatalf("error %q does not name %s", err, reducerBatchClaimFunnelEnabledEnv)
	}
}
