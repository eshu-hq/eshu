package projector

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// RetryableError marks projector failures that should re-enter the durable
// queue instead of becoming terminal on the first failure.
type RetryableError interface {
	error
	Retryable() bool
}

// IsRetryable reports whether the supplied error explicitly opts into bounded
// retry behavior.
func IsRetryable(err error) bool {
	var retryable RetryableError
	if !errors.As(err, &retryable) {
		return false
	}

	return retryable.Retryable()
}

// RetryInjector optionally emits a retryable failure before projection writes.
type RetryInjector interface {
	MaybeFail(scope.IngestionScope, scope.ScopeGeneration) error
}

// RetryOnceInjector emits one retryable failure for each configured
// scope-generation key and then stays quiet on subsequent attempts.
type RetryOnceInjector struct {
	mu        sync.Mutex
	remaining map[string]struct{}
}

// NewRetryOnceInjector builds a one-shot retry injector from a comma-separated
// list of scope-generation keys in <scope_id>:<generation_id> format.
func NewRetryOnceInjector(raw string) (*RetryOnceInjector, error) {
	keys := strings.Split(strings.TrimSpace(raw), ",")
	remaining := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		scopeID, generationID, ok := strings.Cut(key, ":")
		if !ok || strings.TrimSpace(scopeID) == "" || strings.TrimSpace(generationID) == "" {
			return nil, fmt.Errorf("invalid retry-once key %q, want <scope_id>:<generation_id>", key)
		}
		remaining[scopeGenerationKey(strings.TrimSpace(scopeID), strings.TrimSpace(generationID))] = struct{}{}
	}
	if len(remaining) == 0 {
		return nil, nil
	}

	return &RetryOnceInjector{remaining: remaining}, nil
}

// MaybeFail returns one retryable error for each configured key.
func (i *RetryOnceInjector) MaybeFail(scopeValue scope.IngestionScope, generation scope.ScopeGeneration) error {
	if i == nil {
		return nil
	}

	key := scopeGenerationKey(scopeValue.ScopeID, generation.GenerationID)
	i.mu.Lock()
	defer i.mu.Unlock()
	if _, ok := i.remaining[key]; !ok {
		return nil
	}
	delete(i.remaining, key)

	return injectedRetryableError{key: key}
}

type injectedRetryableError struct {
	key string
}

func (e injectedRetryableError) Error() string {
	return fmt.Sprintf("injected retryable projector failure for %s", e.key)
}

func (injectedRetryableError) Retryable() bool {
	return true
}

func scopeGenerationKey(scopeID string, generationID string) string {
	return fmt.Sprintf("%s:%s", scopeID, generationID)
}
