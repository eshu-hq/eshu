package reducer

import "testing"

func TestImplementedDefaultDomainDefinitionsOmitsCodeInterprocWithoutWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		CodeEvidenceHandlers: CodeEvidenceHandlers{
			CodeInterprocEvidenceLoader: stubCodeInterprocEvidenceLoader{},
		},
	})
	for _, def := range definitions {
		if def.Domain == DomainCodeInterprocEvidence {
			t.Fatalf("code_interproc_evidence registered without writer; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesCodeInterprocWhenWired(t *testing.T) {
	t.Parallel()

	loader := stubCodeInterprocEvidenceLoader{}
	writer := &recordingCodeInterprocEvidenceWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		CodeEvidenceHandlers: CodeEvidenceHandlers{
			CodeInterprocEvidenceLoader: loader,
			CodeInterprocEvidenceWriter: writer,
		},
	})

	found := false
	for _, def := range definitions {
		if def.Domain != DomainCodeInterprocEvidence {
			continue
		}
		found = true
		handler, ok := def.Handler.(CodeInterprocEvidenceMaterializationHandler)
		if !ok {
			t.Fatalf("code_interproc_evidence handler type = %T, want CodeInterprocEvidenceMaterializationHandler", def.Handler)
		}
		if handler.Loader == nil || handler.Writer != writer {
			t.Fatal("code_interproc_evidence handler Loader/Writer not wired")
		}
		if !def.Ownership.CanonicalWrite {
			t.Fatal("code_interproc_evidence must declare CanonicalWrite ownership")
		}
	}
	if !found {
		t.Fatal("code_interproc_evidence domain not registered when fully wired")
	}
}

// TestNewDefaultRegistryAcceptsCodeInterprocOwnership proves the interproc domain
// passes the reducer ownership invariant (cross-source, cross-scope, canonical
// write) so registering it in a wired runtime does not fail NewDefaultRegistry.
func TestNewDefaultRegistryAcceptsCodeInterprocOwnership(t *testing.T) {
	t.Parallel()

	registry, err := NewDefaultRegistry(DefaultHandlers{
		CodeEvidenceHandlers: CodeEvidenceHandlers{
			CodeInterprocEvidenceLoader: stubCodeInterprocEvidenceLoader{},
			CodeInterprocEvidenceWriter: &recordingCodeInterprocEvidenceWriter{},
		},
	})
	if err != nil {
		t.Fatalf("NewDefaultRegistry returned error with interproc wired: %v", err)
	}
	if _, ok := registry.Definition(DomainCodeInterprocEvidence); !ok {
		t.Fatal("code_interproc_evidence not registered in default registry when wired")
	}
}
