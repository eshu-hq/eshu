// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

// hasDomain reports whether the definition slice registers the given domain.
func hasDomain(defs []DomainDefinition, domain Domain) (DomainDefinition, bool) {
	for _, d := range defs {
		if d.Domain == domain {
			return d, true
		}
	}
	return DomainDefinition{}, false
}

// TestAppendAdditiveDomainsWiresSecretsIAMGraphProjection proves the
// secrets/IAM graph projection domain registers only when both its FactLoader
// and SecretsIAMGraphWriter dependencies are wired, and that the registered
// handler carries those exact dependencies. Registering it without the writer
// would silently drop every projection intent, so the gate must hold.
func TestAppendAdditiveDomainsWiresSecretsIAMGraphProjection(t *testing.T) {
	t.Parallel()

	loader := fakeFactLoader{}
	writer := &recordingGraphWriter{}

	withWriter := appendAdditiveDomainDefinitions(nil, DefaultHandlers{
		FactLoader: loader,
		SupplyChainSecurityHandlers: SupplyChainSecurityHandlers{
			SecretsIAMGraphWriter: writer,
		},
	})
	def, ok := hasDomain(withWriter, DomainSecretsIAMGraphProjection)
	if !ok {
		t.Fatal("secrets_iam_graph_projection not registered when FactLoader and writer are wired")
	}
	handler, ok := def.Handler.(SecretsIAMGraphProjectionHandler)
	if !ok {
		t.Fatalf("handler type = %T, want SecretsIAMGraphProjectionHandler", def.Handler)
	}
	if handler.FactLoader == nil || handler.Writer == nil {
		t.Fatalf("handler dependencies not wired: loader=%v writer=%v", handler.FactLoader, handler.Writer)
	}
	if !def.TruthContract.Supports(truth.LayerObservedResource) {
		t.Fatal("secrets_iam_graph_projection truth contract does not accept observed_resource source evidence")
	}
	if def.TruthContract.Supports(truth.LayerCanonicalAsset) {
		t.Fatal("secrets_iam_graph_projection truth contract accepts canonical_asset as source evidence")
	}
}

func TestNewDefaultRegistryRegistersSecretsIAMGraphProjection(t *testing.T) {
	t.Parallel()

	_, err := NewDefaultRegistry(DefaultHandlers{
		FactLoader: fakeFactLoader{},
		SupplyChainSecurityHandlers: SupplyChainSecurityHandlers{
			SecretsIAMGraphWriter: &recordingGraphWriter{},
		},
	})
	if err != nil {
		t.Fatalf("NewDefaultRegistry() error = %v, want nil with secrets/IAM graph writer wired", err)
	}
}

// TestAppendAdditiveDomainsSkipsSecretsIAMGraphProjectionWithoutWriter proves
// the projection domain stays unregistered (default OFF) when the
// SecretsIAMGraphWriter is absent, even though the FactLoader is present. This
// is the ADR #1314 §14 gate: live graph writes never activate without an
// explicitly wired, sign-off-gated writer.
func TestAppendAdditiveDomainsSkipsSecretsIAMGraphProjectionWithoutWriter(t *testing.T) {
	t.Parallel()

	defs := appendAdditiveDomainDefinitions(nil, DefaultHandlers{
		FactLoader: fakeFactLoader{},
	})
	if _, ok := hasDomain(defs, DomainSecretsIAMGraphProjection); ok {
		t.Fatal("secrets_iam_graph_projection registered without a wired writer")
	}
}
