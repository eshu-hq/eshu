// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

const projectorRetryOnceScopeGenerationEnv = "ESHU_PROJECTOR_RETRY_ONCE_SCOPE_GENERATION"

func loadProjectorRetryInjector(getenv func(string) string) (failure.RetryInjector, error) {
	if getenv == nil {
		return nil, nil
	}

	raw := strings.TrimSpace(getenv(projectorRetryOnceScopeGenerationEnv))
	if raw == "" {
		return nil, nil
	}

	return failure.NewRetryOnceInjector(raw)
}

func loadProjectorRetryPolicy(getenv func(string) string) (runtimecfg.RetryPolicyConfig, error) {
	return runtimecfg.LoadRetryPolicyConfig(getenv, "PROJECTOR")
}
