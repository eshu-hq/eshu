// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

// TestImplementedDefaultDomainDefinitionsOmitsAWSCloudRuntimeDriftWithoutFencingTokenIssuer
// proves the registration gate treats AWSCloudRuntimeDriftFencingTokenIssuer
// as required alongside the writer (#5875 P1), not merely optional like
// ReadinessChecker: wiring loader+writer without an issuer must still omit
// the domain, rather than register a handler whose every Handle() call would
// hard-error on the missing issuer. Split into its own file (not appended to
// the already-over-cap defaults_test.go) to avoid inflating a pre-existing
// 500-line-cap violation further.
func TestImplementedDefaultDomainDefinitionsOmitsAWSCloudRuntimeDriftWithoutFencingTokenIssuer(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		DriftHandlers: DriftHandlers{
			AWSCloudRuntimeDriftEvidenceLoader: &stubAWSCloudRuntimeDriftEvidenceLoader{},
			AWSCloudRuntimeDriftWriter:         &stubAWSCloudRuntimeDriftFindingWriter{},
			// FencingTokenIssuer deliberately left nil.
		},
	})
	for _, def := range definitions {
		if def.Domain == DomainAWSCloudRuntimeDrift {
			t.Fatal("aws_cloud_runtime_drift registered without a fencing token issuer; want omitted")
		}
	}
}
