// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// Helper file for TestScopeMayHoldWorkloadCandidatesDifferentialLive in
// workload_candidate_precondition_live_test.go: the generated corpus, the
// extraction replay, and the bulk seed. It defines no Test function.

// probeDiffCase is one scope generation: an optional repository fact and its
// file facts, as the JSON text the collector would persist.
type probeDiffCase struct {
	name  string
	repo  string
	files []string
	// mustBeFalse marks shapes that hold only null, empty, or blank signal
	// values and no Jenkinsfile path (or a parsed payload that is not a JSON
	// object): the value-aware probe must answer false.
	// This is the tightness proof; the false-negative check is the safety proof.
	mustBeFalse bool
}

// keyExistenceBaselineSQL is the pre-value-aware probe (#9986 first fix): key
// existence only. It is used only to report how much the value-aware probe
// narrows the answer; no assertion depends on it.
const keyExistenceBaselineSQL = `SELECT EXISTS (
  SELECT 1 FROM fact_records AS repo
  WHERE repo.scope_id = $1 AND repo.generation_id = $2 AND repo.fact_kind = 'repository'
    AND repo.payload->>'graph_id' <> '' AND repo.payload->>'name' <> ''
    AND EXISTS (
      SELECT 1 FROM fact_records AS f
      WHERE f.scope_id = $1 AND f.generation_id = $2 AND f.fact_kind = 'file'
        AND f.payload->>'repo_id' = repo.payload->>'graph_id'
        AND f.payload->'parsed_file_data' ?| $3::text[]))`

// probeDiffValueShapes are the JSON values one parsed_file_data key can hold.
// They cover the null and empty shapes the parsers really emit (QA showed
// k8s_resources stored as JSON null) next to every shape extraction could
// read as a signal.
var probeDiffValueShapes = []struct{ name, json string }{
	{"null", `null`},
	{"empty-array", `[]`},
	{"array-int", `[1]`},
	{"array-object", `[{"kind":"Deployment","namespace":"prod"}]`},
	{"empty-string", `""`},
	{"blank-string", `"   "`},
	{"tab-string", `"\t"`},
	{"nbsp-string", `" "`},
	{"string", `"x"`},
	{"nil-string", `"<nil>"`},
	{"docker-compose", `"docker_compose"`},
	{"padded-docker-compose", `" docker_compose "`},
	{"nbsp-docker-compose", `" docker_compose "`},
	{"github-workflow", `"github_actions_workflow"`},
	{"zero", `0`},
	{"one", `1`},
	{"true", `true`},
	{"false", `false`},
	{"empty-object", `{}`},
	{"object", `{"a":1}`},
}

// probeDiffPaths and probeDiffLanguages are the file-fact fields extraction
// reads next to the parsed payload (Jenkinsfile and workflow conventions, and
// the dockerfile, groovy, yaml language gates).
var (
	probeDiffPaths = []string{
		"x", "Jenkinsfile", " jenkinsfile\t", "JenKinsfile", "Jenkinſfile",
		"ci/Jenkinsfile", ".github/workflows/ci.yml",
	}
	probeDiffLanguages = []string{"", "dockerfile", "groovy", " YAML "}
)

// probeDiffPathIsJenkinsfile reports whether probeDiffPaths[i] is a spelling
// extraction accepts as the Jenkinsfile convention (the first five entries
// after "x"); "ci/Jenkinsfile" and the workflow path are not.
func probeDiffPathIsJenkinsfile(i int) bool { return i >= 1 && i <= 4 }

// probeDiffValueIsEmpty reports whether a shape carries no evidence for the
// key: null or an empty list for an array bucket, null or blank for
// chart_type, anything but a known artifact type for artifact_type.
func probeDiffValueIsEmpty(key, shape string) bool {
	switch key {
	case "chart_type":
		switch shape {
		case "null", "empty-string", "blank-string", "tab-string", "nbsp-string", "nil-string":
			return true
		}
	case "artifact_type":
		switch shape {
		case "docker-compose", "padded-docker-compose", "nbsp-docker-compose", "github-workflow":
			return false
		}
		return true
	default:
		return shape == "null" || shape == "empty-array"
	}
	return false
}

func probeDiffFile(repoID any, language, path, parsed string) string {
	payload := map[string]any{"repo_id": repoID, "language": language, "relative_path": path}
	if parsed != "" {
		payload["parsed_file_data"] = json.RawMessage(parsed)
	}
	out, _ := json.Marshal(payload)
	return string(out)
}

func probeDiffRepo(graphID, name string) string {
	return fmt.Sprintf(`{"graph_id":%s,"name":%s}`, graphID, name)
}

func probeDiffCases() []probeDiffCase {
	const repo = `{"graph_id":"r1","name":"R"}`
	var cases []probeDiffCase
	add := func(name, repoJSON string, files ...string) {
		cases = append(cases, probeDiffCase{name: name, repo: repoJSON, files: files})
	}
	addEmpty := func(name, repoJSON string, files ...string) {
		cases = append(cases, probeDiffCase{name: name, repo: repoJSON, files: files, mustBeFalse: true})
	}
	// One signal key at a time, every value shape, every language and path.
	for _, key := range factload.WorkloadCandidateSignalKeys {
		for _, shape := range probeDiffValueShapes {
			for _, lang := range probeDiffLanguages {
				for pathIndex, path := range probeDiffPaths {
					name := fmt.Sprintf("%s=%s lang=%q path=%q", key, shape.name, lang, path)
					file := probeDiffFile("r1", lang, path, fmt.Sprintf(`{%q:%s}`, key, shape.json))
					if probeDiffValueIsEmpty(key, shape.name) && !probeDiffPathIsJenkinsfile(pathIndex) {
						addEmpty(name, repo, file)
						continue
					}
					add(name, repo, file)
				}
			}
		}
	}
	// No signal key at all: only the path convention can speak.
	for _, parsed := range []string{`{}`, `{"unrelated":[1]}`, `[]`, `"x"`, `null`, ``, `["k8s_resources"]`} {
		// Extraction reads parsed_file_data only when it is a JSON object, so a
		// non-object payload yields no candidate whatever the path says.
		notObject := parsed != `{}` && parsed != `{"unrelated":[1]}`
		for _, lang := range probeDiffLanguages {
			for pathIndex, path := range probeDiffPaths {
				name := fmt.Sprintf("no-key parsed=%s lang=%q path=%q", parsed, lang, path)
				file := probeDiffFile("r1", lang, path, parsed)
				if notObject || !probeDiffPathIsJenkinsfile(pathIndex) {
					addEmpty(name, repo, file)
					continue
				}
				add(name, repo, file)
			}
		}
	}
	// QA shape: every signal key present and null / empty on every file.
	nullish := `{"k8s_resources":null,"argocd_applications":null,"argocd_applicationsets":null,` +
		`"cloudformation_resources":null,"cloudformation_parameters":null,"cloudformation_outputs":null,` +
		`"cloudformation_cross_stack_imports":null,"cloudformation_cross_stack_exports":null,"dockerfile_stages":[]}`
	addEmpty("qa all-null signal keys, many files", repo,
		probeDiffFile("r1", "yaml", "a.yaml", nullish), probeDiffFile("r1", "yaml", "b.yaml", nullish))
	// Several files: a signal only on a later file, a blank chart_type next to
	// a real stage list, and signals split across repositories.
	add("signal on the second file only", repo,
		probeDiffFile("r1", "yaml", "a.yaml", `{"k8s_resources":null}`),
		probeDiffFile("r1", "yaml", "b.yaml", `{"k8s_resources":[{"kind":"Service"}]}`))
	add("blank chart_type beside real stages", repo,
		probeDiffFile("r1", "dockerfile", "Dockerfile", `{"chart_type":"","dockerfile_stages":[{"name":"x"}]}`))
	add("signal belongs to a foreign repo_id", repo,
		probeDiffFile("r2", "yaml", "a.yaml", `{"k8s_resources":[{"kind":"Service"}]}`))
	// Repository identity shapes.
	for _, graphID := range []string{`"r1"`, `null`, `""`, `"  "`, `"<nil>"`, `5`, `"r1 "`} {
		for _, name := range []string{`"R"`, `null`, `""`, `"  "`, `"<nil>"`} {
			add(fmt.Sprintf("identity graph_id=%s name=%s", graphID, name), probeDiffRepo(graphID, name),
				probeDiffFile("r1", "yaml", "a.yaml", `{"k8s_resources":[{"kind":"Service"}]}`),
				probeDiffFile(5, "yaml", "b.yaml", `{"k8s_resources":[{"kind":"Service"}]}`))
		}
	}
	for _, repoID := range []any{nil, "", "  ", "r1", " r1 ", 5} {
		add(fmt.Sprintf("file repo_id=%v", repoID), repo,
			probeDiffFile(repoID, "yaml", "a.yaml", `{"k8s_resources":[{"kind":"Service"}]}`))
	}
	add("no repository fact", "", probeDiffFile("r1", "yaml", "a.yaml", `{"k8s_resources":[{"kind":"Service"}]}`))
	add("repository only", repo)
	return cases
}

// extractProbeDiffCandidates runs production extraction on the case payloads,
// decoded the way the fact loader decodes them.
func extractProbeDiffCandidates(t *testing.T, c probeDiffCase) []reducer.WorkloadCandidate {
	t.Helper()
	var envelopes []facts.Envelope
	decode := func(kind, raw string) {
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatalf("decode %s payload %q: %v", kind, raw, err)
		}
		envelopes = append(envelopes, facts.Envelope{FactKind: kind, Payload: payload})
	}
	if c.repo != "" {
		decode("repository", c.repo)
	}
	for _, file := range c.files {
		decode("file", file)
	}
	candidates, _ := reducer.ExtractWorkloadCandidates(envelopes)
	return candidates
}

// seedProbeDiffCases writes one scope, generation, and fact set per case with
// three bulk statements.
func seedProbeDiffCases(ctx context.Context, t *testing.T, db *sql.DB, cases []probeDiffCase) {
	t.Helper()
	type factRow struct {
		FactID       string          `json:"fact_id"`
		ScopeID      string          `json:"scope_id"`
		GenerationID string          `json:"generation_id"`
		Kind         string          `json:"kind"`
		Payload      json.RawMessage `json:"payload"`
	}
	var scopes []string
	var rows []factRow
	for i, c := range cases {
		scopeID, generationID := fmt.Sprintf("scope-d%d", i), fmt.Sprintf("gen-d%d", i)
		scopes = append(scopes, scopeID)
		if c.repo != "" {
			rows = append(rows, factRow{fmt.Sprintf("d%d-repo", i), scopeID, generationID, "repository", json.RawMessage(c.repo)})
		}
		for j, file := range c.files {
			rows = append(rows, factRow{fmt.Sprintf("d%d-file%d", i, j), scopeID, generationID, "file", json.RawMessage(file)})
		}
	}
	scopeJSON, err := json.Marshal(scopes)
	if err != nil {
		t.Fatalf("marshal scopes: %v", err)
	}
	rowJSON, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal facts: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
SELECT s, 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active' FROM jsonb_array_elements_text($1::jsonb) AS s`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'gen-d' || (ord - 1), s, 'poll', now(), now(), 'active'
FROM jsonb_array_elements_text($1::jsonb) WITH ORDINALITY AS t(s, ord)`,
	} {
		if _, err := db.ExecContext(ctx, stmt, string(scopeJSON)); err != nil {
			t.Fatalf("seed differential scopes: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT r.fact_id, r.scope_id, r.generation_id, r.kind, 's', 'git', 'k', now(), now(), FALSE, r.payload
FROM jsonb_to_recordset($1::jsonb) AS r(fact_id text, scope_id text, generation_id text, kind text, payload jsonb)`,
		string(rowJSON)); err != nil {
		t.Fatalf("seed differential facts: %v", err)
	}
}
