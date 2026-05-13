package collector

import (
	"context"
	"errors"
	"testing"
)

func TestRegistryTransportFailureClassifiesCancellation(t *testing.T) {
	t.Parallel()

	err := RegistryTransportFailure("oci", "", "list_tags", context.Canceled)

	if got := err.FailureClass(); got != RegistryFailureCanceled {
		t.Fatalf("FailureClass() = %q, want %q", got, RegistryFailureCanceled)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("RegistryTransportFailure() does not unwrap context.Canceled")
	}
}
