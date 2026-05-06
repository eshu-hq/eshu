package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestNewRetryOnceInjectorParsesConfiguredKey(t *testing.T) {
	t.Parallel()

	injector, err := NewRetryOnceInjector("scope-123:generation-456")
	if err != nil {
		t.Fatalf("NewRetryOnceInjector() error = %v, want nil", err)
	}

	scopeValue := scope.IngestionScope{ScopeID: "scope-123"}
	generation := scope.ScopeGeneration{GenerationID: "generation-456"}

	err = injector.MaybeFail(scopeValue, generation)
	if err == nil {
		t.Fatal("MaybeFail() error = nil, want retryable error")
	}
	if !IsRetryable(err) {
		t.Fatalf("MaybeFail() retryable = false, want true")
	}
	if err := injector.MaybeFail(scopeValue, generation); err != nil {
		t.Fatalf("MaybeFail() second call error = %v, want nil", err)
	}
}

func TestNewRetryOnceInjectorRejectsInvalidKey(t *testing.T) {
	t.Parallel()

	if _, err := NewRetryOnceInjector("scope-only"); err == nil {
		t.Fatal("NewRetryOnceInjector() error = nil, want non-nil")
	}
}
