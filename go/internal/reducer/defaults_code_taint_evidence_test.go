package reducer

import "testing"

func TestImplementedDefaultDomainDefinitionsOmitsCodeTaintWithoutWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		CodeEvidenceHandlers: CodeEvidenceHandlers{
			CodeTaintEvidenceLoader: stubCodeTaintEvidenceLoader{},
		},
	})
	for _, def := range definitions {
		if def.Domain == DomainCodeTaintEvidence {
			t.Fatalf("code_taint_evidence registered without writer; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesCodeTaintWhenWired(t *testing.T) {
	t.Parallel()

	loader := stubCodeTaintEvidenceLoader{}
	writer := &recordingCodeTaintEvidenceWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		CodeEvidenceHandlers: CodeEvidenceHandlers{
			CodeTaintEvidenceLoader: loader,
			CodeTaintEvidenceWriter: writer,
		},
	})

	found := false
	for _, def := range definitions {
		if def.Domain != DomainCodeTaintEvidence {
			continue
		}
		found = true
		handler, ok := def.Handler.(CodeTaintEvidenceMaterializationHandler)
		if !ok {
			t.Fatalf("code_taint_evidence handler type = %T, want CodeTaintEvidenceMaterializationHandler", def.Handler)
		}
		if handler.Loader == nil || handler.Writer != writer {
			t.Fatal("code_taint_evidence handler Loader/Writer not wired")
		}
		if !def.Ownership.CanonicalWrite {
			t.Fatal("code_taint_evidence must declare CanonicalWrite ownership")
		}
	}
	if !found {
		t.Fatal("code_taint_evidence domain not registered when fully wired")
	}
}
