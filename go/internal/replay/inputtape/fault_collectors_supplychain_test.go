// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape_test

// Collector fault-injection coverage (C-14, #4367) — supply-chain collector
// boundaries. See fault_collectors_test.go for the shared helper, the
// fault-injection rationale, and the active skills.
//
// terraform_state and sbom_attestation reach their upstream through a narrow
// dependency interface (an S3 object client and an OCI Distribution referrer
// client) rather than a raw *http.Client, so their fault is injected at that
// interface seam — the collector's real boundary — with the same inputtape
// timeout fault value.

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry/packageruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/sbomruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/collector/vulnerabilityintelligence"
	"github.com/eshu-hq/eshu/go/internal/replay/inputtape"
)

// TestVulnerabilityIntelligenceCollectorSurfacesInjectedTimeout drives the real
// OSV client (the vulnerability-intelligence collector's HTTP boundary) into a
// boundary timeout.
func TestVulnerabilityIntelligenceCollectorSurfacesInjectedTimeout(t *testing.T) {
	t.Parallel()

	assertSurfacesInjectedTimeout(t, func() error {
		client := vulnerabilityintelligence.NewOSVClient("https://osv.invalid", timeoutFaultClient())
		_, err := client.GetVulnerability(context.Background(), "GHSA-0000-0000-0000")
		return err
	})
}

// TestOCIRegistryCollectorSurfacesInjectedTimeout drives the real OCI
// Distribution client into a boundary timeout.
func TestOCIRegistryCollectorSurfacesInjectedTimeout(t *testing.T) {
	t.Parallel()

	assertSurfacesInjectedTimeout(t, func() error {
		client, err := distribution.NewClient(distribution.ClientConfig{
			BaseURL:     "https://registry.invalid",
			BearerToken: "registry-token",
			Client:      timeoutFaultClient(),
		})
		if err != nil {
			return err
		}
		return client.Ping(context.Background())
	})
}

// TestPackageRegistryCollectorSurfacesInjectedTimeout drives the real package
// registry HTTP metadata provider into a boundary timeout.
func TestPackageRegistryCollectorSurfacesInjectedTimeout(t *testing.T) {
	t.Parallel()

	assertSurfacesInjectedTimeout(t, func() error {
		provider := packageruntime.HTTPMetadataProvider{Client: timeoutFaultClient()}
		_, err := provider.FetchMetadata(context.Background(), packageruntime.TargetConfig{
			MetadataURL: "https://packages.invalid/metadata.json",
		})
		return err
	})
}

// TestSBOMAttestationCollectorSurfacesInjectedTimeout drives the real SBOM
// attestation HTTP document provider into a boundary timeout.
func TestSBOMAttestationCollectorSurfacesInjectedTimeout(t *testing.T) {
	t.Parallel()

	assertSurfacesInjectedTimeout(t, func() error {
		provider := sbomruntime.HTTPProvider{HTTPClient: timeoutFaultClient()}
		_, err := provider.FetchDocument(context.Background(), sbomruntime.TargetConfig{
			ScopeID:     "sbom:instance:prod",
			SourceType:  sbomruntime.SourceTypeConfigured,
			DocumentURL: "https://attestations.invalid/sbom.json",
		})
		return err
	})
}

// faultS3ObjectClient injects the inputtape timeout fault at the terraform-state
// collector's S3 read boundary — the narrow dependency interface the collector
// reads remote state through — reproducing a transport timeout without a live
// S3 backend.
type faultS3ObjectClient struct{ err error }

// GetObject fails every read with the injected fault.
func (f faultS3ObjectClient) GetObject(context.Context, terraformstate.S3GetObjectInput) (terraformstate.S3GetObjectOutput, error) {
	return terraformstate.S3GetObjectOutput{}, f.err
}

// TestTerraformStateCollectorSurfacesInjectedTimeout drives the real S3 state
// source into a boundary timeout at its S3 read seam.
func TestTerraformStateCollectorSurfacesInjectedTimeout(t *testing.T) {
	t.Parallel()

	assertSurfacesInjectedTimeout(t, func() error {
		source, err := terraformstate.NewS3StateSource(terraformstate.S3SourceConfig{
			Bucket: "tfstate-prod",
			Key:    "env/prod/terraform.tfstate",
			Region: "us-east-1",
			Client: faultS3ObjectClient{err: inputtape.ErrFaultTimeout},
		})
		if err != nil {
			return err
		}
		_, _, err = source.Open(context.Background())
		return err
	})
}
