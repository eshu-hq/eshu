package query

import "testing"

func TestCodeHandlerGraphBackendRejectsInvalidConfiguredBackend(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{GraphBackend: GraphBackend("not-a-backend")}
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("graphBackend() did not panic for invalid configured backend")
		}
	}()

	_ = handler.graphBackend()
}

func TestCodeHandlerGraphBackendKeepsZeroValueBackendNeo4jCompatible(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{}
	if got, want := handler.graphBackend(), GraphBackendNeo4j; got != want {
		t.Fatalf("graphBackend() = %q, want %q", got, want)
	}
}
