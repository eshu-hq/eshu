package main

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

const ingesterRetryOnceScopeGenerationEnv = "ESHU_PROJECTOR_RETRY_ONCE_SCOPE_GENERATION"

func loadIngesterRetryInjector(getenv func(string) string) (projector.RetryInjector, error) {
	if getenv == nil {
		return nil, nil
	}

	raw := strings.TrimSpace(getenv(ingesterRetryOnceScopeGenerationEnv))
	if raw == "" {
		return nil, nil
	}

	return projector.NewRetryOnceInjector(raw)
}

func loadIngesterRetryPolicy(getenv func(string) string) (runtimecfg.RetryPolicyConfig, error) {
	return runtimecfg.LoadRetryPolicyConfig(getenv, "PROJECTOR")
}
