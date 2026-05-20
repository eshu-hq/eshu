package mcp

import "testing"

func TestDocumentationToolsAreRegisteredAndRouted(t *testing.T) {
	t.Parallel()

	tools := documentationTools()
	if got, want := len(tools), 3; got != want {
		t.Fatalf("len(documentationTools()) = %d, want %d", got, want)
	}

	cases := []struct {
		name       string
		args       map[string]any
		wantMethod string
		wantPath   string
	}{
		{
			name:       "list_documentation_findings",
			args:       map[string]any{"status": "contradicted", "limit": 25},
			wantMethod: "GET",
			wantPath:   "/api/v0/documentation/findings",
		},
		{
			name:       "get_documentation_evidence_packet",
			args:       map[string]any{"finding_id": "finding:docs:1"},
			wantMethod: "GET",
			wantPath:   "/api/v0/documentation/findings/finding:docs:1/evidence-packet",
		},
		{
			name:       "check_documentation_evidence_packet_freshness",
			args:       map[string]any{"packet_id": "doc-packet:1", "packet_version": "1"},
			wantMethod: "GET",
			wantPath:   "/api/v0/documentation/evidence-packets/doc-packet:1/freshness",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			route, err := resolveRoute(tc.name, tc.args)
			if err != nil {
				t.Fatalf("resolveRoute() error = %v, want nil", err)
			}
			if got := route.method; got != tc.wantMethod {
				t.Fatalf("method = %q, want %q", got, tc.wantMethod)
			}
			if got := route.path; got != tc.wantPath {
				t.Fatalf("path = %q, want %q", got, tc.wantPath)
			}
		})
	}
}
