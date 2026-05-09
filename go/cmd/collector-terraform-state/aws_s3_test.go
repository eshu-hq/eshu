package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSafeS3GetObjectErrorDoesNotLeakLocator(t *testing.T) {
	t.Parallel()

	raw := errors.New("GET https://tfstate-prod.s3.us-east-1.amazonaws.com/prod/app/terraform.tfstate: access denied for bucket tfstate-prod key prod/app/terraform.tfstate")
	err := safeS3GetObjectError(raw)
	if err == nil {
		t.Fatal("safeS3GetObjectError() = nil, want error")
	}
	for _, leaked := range []string{
		"https://tfstate-prod.s3.us-east-1.amazonaws.com/prod/app/terraform.tfstate",
		"tfstate-prod",
		"prod/app/terraform.tfstate",
	} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("safe error = %q, leaked %q", err.Error(), leaked)
		}
	}
}

func TestSafeS3GetObjectErrorPreservesContextCancellation(t *testing.T) {
	t.Parallel()

	if !errors.Is(safeS3GetObjectError(context.Canceled), context.Canceled) {
		t.Fatal("safeS3GetObjectError(context.Canceled) does not preserve cancellation")
	}
}
