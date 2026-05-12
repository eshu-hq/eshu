package ecr

import (
	"context"
	"errors"
	"fmt"
	"strings"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/smithy-go"
)

// AuthorizationTokenAPI is the narrow ECR API used to mint Distribution
// credentials.
type AuthorizationTokenAPI interface {
	GetAuthorizationToken(
		ctx context.Context,
		input *ecr.GetAuthorizationTokenInput,
		optFns ...func(*ecr.Options),
	) (*ecr.GetAuthorizationTokenOutput, error)
}

// DistributionCredentials are ECR credentials ready for OCI Distribution basic
// auth.
type DistributionCredentials struct {
	Username      string
	Password      string
	ProxyEndpoint string
}

// GetDistributionCredentials converts ECR authorization data into OCI
// Distribution credentials.
func GetDistributionCredentials(
	ctx context.Context,
	client AuthorizationTokenAPI,
) (DistributionCredentials, error) {
	if client == nil {
		return DistributionCredentials{}, fmt.Errorf("ecr authorization client is required")
	}

	output, err := client.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return DistributionCredentials{}, fmt.Errorf("get ecr authorization token: %w", safeAuthorizationError(err))
	}
	if output == nil || len(output.AuthorizationData) == 0 {
		return DistributionCredentials{}, fmt.Errorf("get ecr authorization token: no authorization data")
	}

	data := output.AuthorizationData[0]
	username, password, err := BasicAuthFromAuthorizationToken(awsv2.ToString(data.AuthorizationToken))
	if err != nil {
		return DistributionCredentials{}, err
	}
	return DistributionCredentials{
		Username:      username,
		Password:      password,
		ProxyEndpoint: strings.TrimRight(awsv2.ToString(data.ProxyEndpoint), "/"),
	}, nil
}

func safeAuthorizationError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return fmt.Errorf("aws ecr api error code=%s fault=%s", apiErr.ErrorCode(), apiErr.ErrorFault())
	}
	return fmt.Errorf("aws ecr authorization request failed")
}
