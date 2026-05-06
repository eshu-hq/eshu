package runtime

import (
	"fmt"
	"strings"
	"time"
)

const (
	defaultRetryMaxAttempts = 3
	defaultRetryDelay       = 30 * time.Second
)

// RetryPolicyConfig captures bounded retry settings for one runtime stage.
type RetryPolicyConfig struct {
	MaxAttempts int
	RetryDelay  time.Duration
}

// LoadRetryPolicyConfig reads a bounded retry policy using the supplied stage
// prefix, for example PROJECTOR or REDUCER.
func LoadRetryPolicyConfig(getenv func(string) string, stagePrefix string) (RetryPolicyConfig, error) {
	stagePrefix = strings.TrimSpace(stagePrefix)
	if stagePrefix == "" {
		return RetryPolicyConfig{}, fmt.Errorf("retry policy stage prefix is required")
	}

	maxAttempts, err := intEnvOrDefault(
		getenv,
		fmt.Sprintf("ESHU_%s_MAX_ATTEMPTS", stagePrefix),
		defaultRetryMaxAttempts,
	)
	if err != nil {
		return RetryPolicyConfig{}, err
	}
	retryDelay, err := durationEnvOrDefault(
		getenv,
		fmt.Sprintf("ESHU_%s_RETRY_DELAY", stagePrefix),
		defaultRetryDelay,
	)
	if err != nil {
		return RetryPolicyConfig{}, err
	}
	if maxAttempts <= 0 {
		return RetryPolicyConfig{}, fmt.Errorf("ESHU_%s_MAX_ATTEMPTS must be positive", stagePrefix)
	}
	if retryDelay <= 0 {
		return RetryPolicyConfig{}, fmt.Errorf("ESHU_%s_RETRY_DELAY must be positive", stagePrefix)
	}

	return RetryPolicyConfig{
		MaxAttempts: maxAttempts,
		RetryDelay:  retryDelay,
	}, nil
}
