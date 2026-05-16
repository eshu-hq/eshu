package reducer

import "testing"

func TestImplementedDefaultDomainDefinitionsOmitsCICDRunCorrelationWithoutAdapters(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{})
	for _, def := range definitions {
		if def.Domain == DomainCICDRunCorrelation {
			t.Fatalf("ci_cd_run_correlation registered without adapters; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesCICDRunCorrelationWhenAdaptersPresent(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingCICDRunCorrelationWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:               loader,
		CICDRunCorrelationWriter: writer,
	})
	found := false
	for _, def := range definitions {
		if def.Domain == DomainCICDRunCorrelation {
			found = true
			handler, ok := def.Handler.(CICDRunCorrelationHandler)
			if !ok {
				t.Fatalf("ci_cd_run_correlation handler type = %T, want CICDRunCorrelationHandler", def.Handler)
			}
			if handler.FactLoader != loader {
				t.Fatal("ci_cd_run_correlation handler FactLoader was not wired")
			}
			if handler.Writer != writer {
				t.Fatal("ci_cd_run_correlation handler Writer was not wired")
			}
		}
	}
	if !found {
		t.Fatal("ci_cd_run_correlation not registered after wiring loader+writer")
	}
}
