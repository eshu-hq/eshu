package ecr

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/smithy-go"
)

type fakeAuthorizationTokenAPI struct {
	t           *testing.T
	output      *ecr.GetAuthorizationTokenOutput
	err         error
	calledCount int
}

func (f *fakeAuthorizationTokenAPI) GetAuthorizationToken(
	_ context.Context,
	input *ecr.GetAuthorizationTokenInput,
	_ ...func(*ecr.Options),
) (*ecr.GetAuthorizationTokenOutput, error) {
	f.calledCount++
	if input == nil {
		f.t.Fatal("input is nil")
	}
	return f.output, f.err
}

func TestGetDistributionCredentialsDecodesAuthorizationToken(t *testing.T) {
	t.Parallel()

	token := base64.StdEncoding.EncodeToString([]byte("AWS:secret-password"))
	client := &fakeAuthorizationTokenAPI{
		t: t,
		output: &ecr.GetAuthorizationTokenOutput{
			AuthorizationData: []types.AuthorizationData{{
				AuthorizationToken: awsv2.String(token),
				ProxyEndpoint:      awsv2.String("https://123456789012.dkr.ecr.us-east-1.amazonaws.com/"),
			}},
		},
	}

	credentials, err := GetDistributionCredentials(context.Background(), client)
	if err != nil {
		t.Fatalf("GetDistributionCredentials() error = %v", err)
	}
	if credentials.Username != "AWS" || credentials.Password != "secret-password" {
		t.Fatalf("credentials = %#v, want AWS basic auth", credentials)
	}
	if credentials.ProxyEndpoint != "https://123456789012.dkr.ecr.us-east-1.amazonaws.com" {
		t.Fatalf("ProxyEndpoint = %q", credentials.ProxyEndpoint)
	}
	if client.calledCount != 1 {
		t.Fatalf("calledCount = %d, want 1", client.calledCount)
	}
}

func TestGetDistributionCredentialsRequiresAuthorizationData(t *testing.T) {
	t.Parallel()

	_, err := GetDistributionCredentials(context.Background(), &fakeAuthorizationTokenAPI{
		t:      t,
		output: &ecr.GetAuthorizationTokenOutput{},
	})
	if err == nil {
		t.Fatal("GetDistributionCredentials() error = nil")
	}
}

func TestSafeAuthorizationErrorDoesNotLeakRequestDetails(t *testing.T) {
	t.Parallel()

	err := safeAuthorizationError(errors.New("POST https://api.ecr.us-east-1.amazonaws.com secret account details"))
	if err == nil {
		t.Fatal("safeAuthorizationError() = nil")
	}
	for _, leaked := range []string{"https://api.ecr.us-east-1.amazonaws.com", "secret account"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("safe error = %q, leaked %q", err.Error(), leaked)
		}
	}
}

func TestSafeAuthorizationErrorPreservesContext(t *testing.T) {
	t.Parallel()

	if !errors.Is(safeAuthorizationError(context.DeadlineExceeded), context.DeadlineExceeded) {
		t.Fatal("safeAuthorizationError(context.DeadlineExceeded) did not preserve context error")
	}
}

func TestSafeAuthorizationErrorKeepsAPICode(t *testing.T) {
	t.Parallel()

	err := safeAuthorizationError(&smithy.GenericAPIError{
		Code:    "AccessDeniedException",
		Message: "nope",
		Fault:   smithy.FaultClient,
	})
	if err == nil || !strings.Contains(err.Error(), "AccessDeniedException") {
		t.Fatalf("safeAuthorizationError() = %v, want API code", err)
	}
}
