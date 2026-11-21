// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

const ingesterRetryOnceScopeGenerationEnv = "ESHU_PROJECTOR_RETRY_ONCE_SCOPE_GENERATION"

func loadIngesterRetryInjector(getenv func(string) string) (failure.RetryInjector, error) {
	if getenv == nil {
		return nil, nil
	}

	raw := strings.TrimSpace(getenv(ingesterRetryOnceScopeGenerationEnv))
	if raw == "" {
		return nil, nil
	}

	return failure.NewRetryOnceInjector(raw)
}

func loadIngesterRetryPolicy(getenv func(string) string) (runtimecfg.RetryPolicyConfig, error) {
	return runtimecfg.LoadRetryPolicyConfig(getenv, "PROJECTOR")
}

// ingesterProjectorAdmissionMaxAttemptsEnv bounds admission-timeout waits
// per projector work item (#10671). Unset keeps the queue default (48
// waits, ~44-46h of 5-minute waits); an explicit value must be positive.
// Zero or negative fails startup: there is no disable mode, because an
// unbounded admission budget would reintroduce the infinite worker
// parking #10485 removed. Loaded beside ESHU_REDUCER_ADMISSION_MAX_WAIT;
// the budget itself is enforced by ProjectorQueue.Fail.
const ingesterProjectorAdmissionMaxAttemptsEnv = "ESHU_REDUCER_ADMISSION_MAX_ATTEMPTS"

func loadIngesterProjectorAdmissionMaxAttempts(getenv func(string) string) (int, error) {
	if getenv == nil {
		return 0, nil
	}
	raw := strings.TrimSpace(getenv(ingesterProjectorAdmissionMaxAttemptsEnv))
	if raw == "" {
		return 0, nil
	}
	maxAttempts, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", ingesterProjectorAdmissionMaxAttemptsEnv, err)
	}
	if maxAttempts <= 0 {
		return 0, fmt.Errorf("%s must be positive", ingesterProjectorAdmissionMaxAttemptsEnv)
	}
	return maxAttempts, nil
}
