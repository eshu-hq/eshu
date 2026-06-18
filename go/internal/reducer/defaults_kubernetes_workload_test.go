package reducer

import "testing"

// TestImplementedDefaultDomainDefinitionsOmitsKubernetesWorkloadMaterializationWithoutAdapters
// proves the additive kubernetes_workload_materialization domain is not
// registered when its node writer is absent, so an intent is never silently
// dropped by a handler that cannot write nodes.
func TestImplementedDefaultDomainDefinitionsOmitsKubernetesWorkloadMaterializationWithoutAdapters(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{})
	for _, def := range definitions {
		if def.Domain == DomainKubernetesWorkloadMaterialization {
			t.Fatalf("kubernetes_workload_materialization registered without adapters; want omitted to avoid silent intent drops")
		}
	}
}

// TestImplementedDefaultDomainDefinitionsIncludesKubernetesWorkloadMaterializationWhenAdaptersPresent
// proves the domain is registered and its handler wired once a FactLoader and a
// KubernetesWorkloadNodeWriter are supplied, and that the canonical-nodes phase
// publisher is threaded through so the later edge slice can gate on it.
func TestImplementedDefaultDomainDefinitionsIncludesKubernetesWorkloadMaterializationWhenAdaptersPresent(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingKubernetesWorkloadNodeWriter{}
	publisher := &recordingGraphProjectionPhasePublisher{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                    loader,
		GraphProjectionPhasePublisher: publisher,
		KubernetesHandlers: KubernetesHandlers{
			KubernetesWorkloadNodeWriter: writer,
		},
	})
	found := false
	for _, def := range definitions {
		if def.Domain == DomainKubernetesWorkloadMaterialization {
			found = true
			handler, ok := def.Handler.(KubernetesWorkloadMaterializationHandler)
			if !ok {
				t.Fatalf("kubernetes_workload_materialization handler type = %T, want KubernetesWorkloadMaterializationHandler", def.Handler)
			}
			if handler.FactLoader != loader {
				t.Fatal("kubernetes_workload_materialization handler FactLoader was not wired")
			}
			if handler.NodeWriter != writer {
				t.Fatal("kubernetes_workload_materialization handler NodeWriter was not wired")
			}
			if handler.PhasePublisher != publisher {
				t.Fatal("kubernetes_workload_materialization handler PhasePublisher was not wired (the edge slice gates on this)")
			}
		}
	}
	if !found {
		t.Fatal("kubernetes_workload_materialization not registered after wiring loader+node writer")
	}
}
