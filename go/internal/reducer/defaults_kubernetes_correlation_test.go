package reducer

import "testing"

// TestImplementedDefaultDomainDefinitionsOmitsKubernetesCorrelationWithoutAdapters
// proves the additive kubernetes_correlation domain is not registered when its
// writer is absent, so an intent is never silently dropped by a handler that
// cannot write.
func TestImplementedDefaultDomainDefinitionsOmitsKubernetesCorrelationWithoutAdapters(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{})
	for _, def := range definitions {
		if def.Domain == DomainKubernetesCorrelation {
			t.Fatalf("kubernetes_correlation registered without adapters; want omitted to avoid silent intent drops")
		}
	}
}

// TestImplementedDefaultDomainDefinitionsIncludesKubernetesCorrelationWhenAdaptersPresent
// proves the domain is registered and its handler wired once a FactLoader and a
// KubernetesCorrelationWriter are supplied.
func TestImplementedDefaultDomainDefinitionsIncludesKubernetesCorrelationWhenAdaptersPresent(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingKubernetesCorrelationWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader: loader,
		KubernetesHandlers: KubernetesHandlers{
			KubernetesCorrelationWriter: writer,
		},
	})
	found := false
	for _, def := range definitions {
		if def.Domain == DomainKubernetesCorrelation {
			found = true
			handler, ok := def.Handler.(KubernetesCorrelationHandler)
			if !ok {
				t.Fatalf("kubernetes_correlation handler type = %T, want KubernetesCorrelationHandler", def.Handler)
			}
			if handler.FactLoader != loader {
				t.Fatal("kubernetes_correlation handler FactLoader was not wired")
			}
			if handler.Writer != writer {
				t.Fatal("kubernetes_correlation handler Writer was not wired")
			}
		}
	}
	if !found {
		t.Fatal("kubernetes_correlation not registered after wiring loader+writer")
	}
}
