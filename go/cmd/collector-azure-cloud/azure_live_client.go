package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud/azureruntime"
)

// newAzureLiveProviderFactory builds the read-only live Resource Graph provider
// factory for the claimed-live runner. It is a package var so tests can inject a
// gated or fake factory without live Azure access. The credential is resolved
// from the ambient workload identity (managed identity, workload identity
// federation, or environment), never from the configuration document; the
// credentialRef is a name only and gates the factory so the runner never falls
// back to an unnamed credential silently.
var newAzureLiveProviderFactory = defaultAzureLiveProviderFactory

// defaultAzureLiveProviderFactory resolves the Azure default credential and
// wraps the official Resource Graph SDK client behind the read-only
// LiveProviderFactory. It performs no live Azure call itself; the runner issues
// reads only after the workflow coordinator grants a claim.
func defaultAzureLiveProviderFactory(
	_ context.Context,
	credentialRef string,
) (azureruntime.PageProviderFactory, error) {
	if strings.TrimSpace(credentialRef) == "" {
		return nil, errors.New("azure live credential_ref is required")
	}
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azure default credential unavailable: %w", err)
	}
	client, err := azureruntime.NewAzureSDKResourceGraphClientFromCredential(credential, nil)
	if err != nil {
		return nil, err
	}
	return azureruntime.LiveProviderFactory{ResourceGraphClient: client}, nil
}
