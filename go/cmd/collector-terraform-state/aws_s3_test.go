package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

func TestS3OutputETagPreservesSDKValue(t *testing.T) {
	t.Parallel()

	output := &s3.GetObjectOutput{ETag: awsv2.String(`"opaque-etag"`)}

	if got, want := s3OutputETag(output), `"opaque-etag"`; got != want {
		t.Fatalf("s3OutputETag() = %q, want %q", got, want)
	}
}

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

func TestSafeS3GetObjectErrorMapsNotModified(t *testing.T) {
	t.Parallel()

	err := safeS3GetObjectError(&smithy.GenericAPIError{
		Code:    "NotModified",
		Message: "object not modified",
		Fault:   smithy.FaultClient,
	})

	if !errors.Is(err, terraformstate.ErrStateNotModified) {
		t.Fatalf("safeS3GetObjectError(NotModified) = %v, want ErrStateNotModified", err)
	}
}
