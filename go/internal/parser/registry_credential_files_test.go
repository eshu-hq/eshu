// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

// TestCredentialFilesRouteToDedicatedDefinitions pins the #8812 A1 binding
// precondition on #8833: *.tfvars, *.tfvars.json, and .yarnrc.yml bind through
// their own definitions, so a content-store policy on them never governs every
// .tf, .hcl, .yaml, or .yml body.
func TestCredentialFilesRouteToDedicatedDefinitions(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	cases := []struct{ path, key, language string }{
		{"infra/terraform.tfvars", "tfvars", "hcl"},
		{"env/prod.auto.tfvars", "tfvars", "hcl"},
		{"infra/terraform.tfvars.json", "tfvars", "hcl"},
		{"env/STAGE.TFVARS.JSON", "tfvars", "hcl"},
		{"infra/main.tf", "hcl", "hcl"},
		{"live/terragrunt.hcl", "hcl", "hcl"},
		{".yarnrc.yml", "yarnrc", "yaml"},
		{"packages/web/.yarnrc.yml", "yarnrc", "yaml"},
		{".YARNRC.YML", "yarnrc", "yaml"},
		{"config.yml", "yaml", "yaml"},
		{"yarnrc.yml", "yaml", "yaml"},
		{".yarnrc.yaml", "yaml", "yaml"},
	}
	for _, tc := range cases {
		definition, ok := registry.LookupByPath(filepath.FromSlash(tc.path))
		if !ok {
			t.Errorf("%s: no definition", tc.path)
			continue
		}
		if definition.ParserKey != tc.key || definition.Language != tc.language {
			t.Errorf("%s: got %s/%s, want %s/%s", tc.path, definition.ParserKey, definition.Language, tc.key, tc.language)
		}
	}
}

// TestDedicatedCredentialDefinitionsParseLikeTheirEngines proves the move is
// routing only: the tfvars definition parses with the hcl engine and the
// yarnrc definition with the yaml engine, so the parser payload is identical.
func TestDedicatedCredentialDefinitionsParseLikeTheirEngines(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	files := map[string]string{
		"prod.tfvars":      "region = \"us-east-1\"\nservices = {\n  pagerduty = { escalation_policy = \"P1\" }\n}\n",
		"prod.tfvars.json": "{\"region\": \"us-east-1\", \"replicas\": 2}\n",
		".yarnrc.yml":      "nodeLinker: node-modules\nnpmScopes:\n  acme:\n    npmRegistryServer: \"https://npm.example.com\"\n",
	}
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	registry := DefaultRegistry()
	for name, body := range files {
		path := filepath.Join(repoRoot, name)
		writeTestFile(t, path, body)
		dedicated, ok := registry.LookupByPath(path)
		if !ok {
			t.Fatalf("%s: no definition", name)
		}
		engineKey := map[string]string{"tfvars": "hcl", "yarnrc": "yaml"}[dedicated.ParserKey]
		if engineKey == "" {
			t.Fatalf("%s routed to %q, want tfvars or yarnrc", name, dedicated.ParserKey)
		}
		original, ok := registry.LookupByParserKey(engineKey)
		if !ok {
			t.Fatalf("no %s definition", engineKey)
		}
		// Both payload and error must match: the hcl engine reads a
		// .tfvars.json body with HCL's JSON syntax (#9048), and the move
		// must not change that either.
		got, gotErr := engine.parseDefinition(repoRoot, dedicated, path, false, Options{})
		want, wantErr := engine.parseDefinition(repoRoot, original, path, false, Options{})
		if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
			t.Errorf("%s: %s error %v, %s error %v", name, dedicated.ParserKey, gotErr, engineKey, wantErr)
		}
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(want)
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("%s: %s payload differs from %s payload:\n got  %s\n want %s", name, dedicated.ParserKey, engineKey, gotJSON, wantJSON)
		}
	}
}

// TestDefaultRegistryBindsCredentialFilePolicies pins the #8833 bindings:
// tfvars stores an allowlist skeleton, and yarnrc stores marker redaction over
// Yarn's credential settings (npmAuthToken and npmAuthIdent at any nesting) and
// the userinfo of its URL settings. No other definition binds a policy.
func TestDefaultRegistryBindsCredentialFilePolicies(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	tfvars := registry.ContentStoreForPath("env/prod.tfvars")
	if tfvars.Policy != ContentStoreRedact || tfvars.Redactor != RedactorSkeleton {
		t.Errorf("tfvars policy = %+v, want redact/skeleton", tfvars)
	}
	if json := registry.ContentStoreForPath("env/prod.tfvars.json"); json.Redactor != RedactorSkeleton {
		t.Errorf("tfvars.json policy = %+v, want redact/skeleton", json)
	}
	yarnrc := registry.ContentStoreForPath("pkg/.yarnrc.yml")
	if yarnrc.Policy != ContentStoreRedact || yarnrc.Redactor != RedactorMarker {
		t.Fatalf("yarnrc policy = %+v, want redact/marker", yarnrc)
	}
	if got, want := fmt.Sprint(yarnrc.CredentialKeys), "[npmAuthIdent npmAuthToken]"; got != want {
		t.Errorf("yarnrc credential keys = %s, want %s", got, want)
	}
	if got, want := fmt.Sprint(yarnrc.URLUserinfoKeys), "[httpProxy httpsProxy npmPublishRegistry npmRegistryServer]"; got != want {
		t.Errorf("yarnrc URL userinfo keys = %s, want %s", got, want)
	}
	for _, path := range []string{"main.tf", "live/terragrunt.hcl", "config.yml", "app.yaml"} {
		if got := registry.ContentStoreForPath(path); got.Policy != ContentStoreStore {
			t.Errorf("%s policy = %+v, want store: the binding must not reach hcl or yaml", path, got)
		}
	}
	for _, definition := range registry.Definitions() {
		if definition.ParserKey == "tfvars" || definition.ParserKey == "yarnrc" {
			continue
		}
		if definition.ParserKey == "npmrc" || definition.ParserKey == "gradle_wrapper" || definition.ParserKey == "php_config" || definition.ParserKey == "runtime_txt" || definition.ParserKey == "sbt_properties" || definition.ParserKey == "dotnet_global_json" || definition.ParserKey == "perl_cpanfile" {
			if definition.ContentStore.Policy != ContentStoreMetadata {
				t.Errorf("%s policy = %+v, want metadata (#8978, #8822, #9304, #9308)", definition.ParserKey, definition.ContentStore)
			}
			continue
		}
		if definition.ContentStore.Policy != ContentStoreStore {
			t.Errorf("%s binds %q; only the listed credential files bind a non-store policy", definition.ParserKey, definition.ContentStore.Policy)
		}
	}
}

// TestCredentialFilePoliciesWinEveryDispatch pins the #8833 security review:
// a name another dispatch rule also claims (a Dockerfile or Jenkinsfile prefix,
// a META-INF/services path) still gets the credential file's content-store
// policy, so no naming choice stores the file verbatim.
func TestCredentialFilePoliciesWinEveryDispatch(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for path, want := range map[string]string{
		"Dockerfile.tfvars":                   "tfvars",
		"Jenkinsfile.tfvars":                  "tfvars",
		"Dockerfile.tfvars.json":              "tfvars",
		"src/main/META-INF/services/x.tfvars": "tfvars",
		"META-INF/services/.yarnrc.yml":       "yarnrc",
		"lib/META-INF/services/.npmrc":        "npmrc",
		"conf/x.tfvars":                       "tfvars",
	} {
		definition, ok := registry.lookupPath(path)
		if !ok || definition.ParserKey != want {
			t.Errorf("%s dispatches to %q (ok=%v), want %s", path, definition.ParserKey, ok, want)
		}
		if got := registry.ContentStoreForPath(path); got.Policy == ContentStoreStore || got.Policy == "" {
			t.Errorf("%s content store = %+v, want the %s policy", path, got, want)
		}
	}
}
