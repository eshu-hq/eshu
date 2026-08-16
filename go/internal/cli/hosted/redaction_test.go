// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hosted

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOnboardArtifactRedactionCanary is the redaction contract for the
// shareable artifact. It plants a distinct sentinel in each place a secret can
// enter, then asserts the sentinel is ABSENT from both renderings and from the
// file WriteArtifact persists. Sentinels are planted inside VALUES (an endpoint
// password, a bearer token), not under sensitive-looking keys, because a canary
// keyed on field names cannot see a secret embedded in an ordinary value.
//
// What this pins as redacted: the bearer token value everywhere, and any
// userinfo embedded in the resolved endpoint, in both the api_url and the
// derived mcp_url. What it deliberately does NOT claim: the endpoint host, port
// and path survive by design so an operator can recognize the target, and stage
// details carry seam-supplied error text verbatim — see the package README.
func TestOnboardArtifactRedactionCanary(t *testing.T) {
	t.Parallel()
	const (
		endpointPassword = "CANARY-endpoint-password-must-not-appear"
		tokenValue       = "CANARY-bearer-token-must-not-appear"
	)

	opts := narrowOnboardOptions()
	opts.ServiceURL = "https://svc-user:" + endpointPassword + "@eshu.example.com"
	opts.APIKey = tokenValue

	artifact, err := ExecuteOnboard(okDeps(), opts)
	if err != nil {
		t.Fatalf("ExecuteOnboard() err = %v", err)
	}

	markdown, err := RenderArtifactMarkdown(artifact)
	if err != nil {
		t.Fatalf("RenderArtifactMarkdown: %v", err)
	}
	jsonBytes, err := RenderArtifactJSON(artifact)
	if err != nil {
		t.Fatalf("RenderArtifactJSON: %v", err)
	}

	var terminal bytes.Buffer
	RenderArtifactTerminal(&terminal, artifact, nil)

	dir := t.TempDir()
	mdPath := filepath.Join(dir, "artifact.md")
	jsonPath := filepath.Join(dir, "artifact.json")
	if err := WriteArtifact(artifact, "md", mdPath); err != nil {
		t.Fatalf("WriteArtifact md: %v", err)
	}
	if err := WriteArtifact(artifact, "json", jsonPath); err != nil {
		t.Fatalf("WriteArtifact json: %v", err)
	}
	mdFile, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read markdown artifact: %v", err)
	}
	jsonFile, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read json artifact: %v", err)
	}

	renderings := map[string]string{
		"markdown":      markdown,
		"json":          string(jsonBytes),
		"terminal":      terminal.String(),
		"markdown file": string(mdFile),
		"json file":     string(jsonFile),
	}
	for surface, body := range renderings {
		for _, sentinel := range []string{endpointPassword, tokenValue} {
			if strings.Contains(body, sentinel) {
				t.Fatalf("%s rendering leaked sentinel %q:\n%s", surface, sentinel, body)
			}
		}
	}

	// The redaction must survive on the specific fields, not merely because the
	// artifact came out empty.
	if !strings.Contains(artifact.APIURL, "eshu.example.com") {
		t.Fatalf("APIURL = %q, want the recognizable host to survive redaction", artifact.APIURL)
	}
	if !strings.Contains(artifact.MCPURL, "/mcp/message") {
		t.Fatalf("MCPURL = %q, want the derived MCP transport path", artifact.MCPURL)
	}
	if !strings.Contains(markdown, "eshu.example.com") {
		t.Fatal("markdown rendering dropped the endpoint entirely; redaction must keep the host")
	}
}

// TestOnboardRefusedArtifactRedactionCanary proves the refusal path — where the
// artifact is built without ever contacting the service — redacts the same way.
// The refused artifact is still handed to a team, so it is not a lesser surface.
func TestOnboardRefusedArtifactRedactionCanary(t *testing.T) {
	t.Parallel()
	const (
		endpointPassword = "CANARY-refused-endpoint-password"
		tokenValue       = "CANARY-refused-bearer-token"
	)

	opts := narrowOnboardOptions()
	opts.Rules = []RepoRule{{Kind: RulePattern, Value: "acme/*"}}
	opts.ServiceURL = "https://svc-user:" + endpointPassword + "@eshu.example.com"
	opts.APIKey = tokenValue

	artifact, err := ExecuteOnboard(okDeps(), opts)
	if err == nil {
		t.Fatal("expected the broad rule set to be refused")
	}

	markdown, mdErr := RenderArtifactMarkdown(artifact)
	if mdErr != nil {
		t.Fatalf("RenderArtifactMarkdown: %v", mdErr)
	}
	jsonBytes, jsonErr := RenderArtifactJSON(artifact)
	if jsonErr != nil {
		t.Fatalf("RenderArtifactJSON: %v", jsonErr)
	}
	for surface, body := range map[string]string{"markdown": markdown, "json": string(jsonBytes)} {
		for _, sentinel := range []string{endpointPassword, tokenValue} {
			if strings.Contains(body, sentinel) {
				t.Fatalf("refused %s rendering leaked sentinel %q:\n%s", surface, sentinel, body)
			}
		}
	}
	if !strings.Contains(artifact.APIURL, "eshu.example.com") {
		t.Fatalf("refused APIURL = %q, want the recognizable host to survive", artifact.APIURL)
	}
}

// TestOnboardSnippetUsesRedactedEndpoint is the focused regression for the leak
// the redaction canary caught: the artifact redacted api_url and mcp_url but
// embedded the raw endpoint inside the client setup snippet, so an endpoint
// carrying userinfo shipped the password to the team in the very block they are
// told to copy. The snippet's URL must now equal the artifact's own mcp_url.
func TestOnboardSnippetUsesRedactedEndpoint(t *testing.T) {
	t.Parallel()
	opts := narrowOnboardOptions()
	opts.ServiceURL = "https://svc-user:s3cr3t@eshu.example.com"

	artifact, err := ExecuteOnboard(okDeps(), opts)
	if err != nil {
		t.Fatalf("ExecuteOnboard() err = %v", err)
	}
	if strings.TrimSpace(artifact.SetupSnippet) == "" {
		t.Fatal("artifact carries no setup snippet; the assertion below would be vacuous")
	}
	if strings.Contains(artifact.SetupSnippet, "s3cr3t") {
		t.Fatalf("setup snippet leaked the endpoint password:\n%s", artifact.SetupSnippet)
	}
	if !strings.Contains(artifact.SetupSnippet, artifact.MCPURL) {
		t.Fatalf("setup snippet URL disagrees with mcp_url %q:\n%s", artifact.MCPURL, artifact.SetupSnippet)
	}
}

// TestOnboardArtifactRedactsEndpointCredentials proves any embedded
// userinfo in the resolved endpoint is redacted before it lands in the artifact.
func TestOnboardArtifactRedactsEndpointCredentials(t *testing.T) {
	t.Parallel()
	opts := narrowOnboardOptions()
	opts.ServiceURL = "https://user:s3cr3t@eshu.example.com"
	artifact, err := ExecuteOnboard(okDeps(), opts)
	if err != nil {
		t.Fatalf("ExecuteOnboard() err = %v", err)
	}
	if strings.Contains(artifact.APIURL, "s3cr3t") {
		t.Fatalf("API URL leaked embedded credentials: %q", artifact.APIURL)
	}
	if strings.Contains(artifact.MCPURL, "s3cr3t") {
		t.Fatalf("MCP URL leaked embedded credentials: %q", artifact.MCPURL)
	}
}
