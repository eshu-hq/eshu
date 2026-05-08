package query

import "testing"

func TestDeadCodeIsTestFileExcludesJavaScriptTestRunnerFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{name: "cypress config", path: "cypress.config.ts"},
		{name: "cypress e2e spec", path: "cypress/e2e/paths/version.cy.ts"},
		{name: "cypress support", path: "cypress/support/commands.ts"},
		{name: "lab test", path: "server/resources/client.lab.ts"},
		{name: "tool config", path: "tsup.config.ts"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := map[string]any{"file_path": tt.path}
			if !deadCodeIsTestFile(result, nil) {
				t.Fatalf("deadCodeIsTestFile(%q) = false, want true", tt.path)
			}
		})
	}
}
