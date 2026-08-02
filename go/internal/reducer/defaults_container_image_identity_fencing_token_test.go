// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

// TestImplementedDefaultDomainDefinitionsOmitsContainerImageIdentityWithoutFencingTokenIssuer
// proves the registration gate treats ContainerImageIdentityFencingTokenIssuer
// as required alongside the writer (#5874), not silently optional: wiring
// loader+writer without an issuer must still omit the domain, rather than
// register a handler whose every Handle() call would hard-error on the
// missing issuer. Mirrors
// TestImplementedDefaultDomainDefinitionsOmitsAWSCloudRuntimeDriftWithoutFencingTokenIssuer.
// Split into its own file (not appended to the already-over-cap
// defaults_test.go) to avoid inflating a pre-existing 500-line-cap violation
// further.
func TestImplementedDefaultDomainDefinitionsOmitsContainerImageIdentityWithoutFencingTokenIssuer(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                   &stubFactLoader{},
		ContainerImageIdentityWriter: &recordingContainerImageIdentityWriter{},
		// FencingTokenIssuer deliberately left nil.
	})
	for _, def := range definitions {
		if def.Domain == DomainContainerImageIdentity {
			t.Fatal("container_image_identity registered without a fencing token issuer; want omitted")
		}
	}
}
