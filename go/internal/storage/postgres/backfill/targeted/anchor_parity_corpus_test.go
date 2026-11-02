// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package targeted

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Repository ids in the real 10-character form. Every one ties the "repository"
// token, which is the shape that made the anchor predicate match the corpus.
const (
	parityRepoPayments   = "repository:r_9a7e0001"
	parityRepoInventory  = "repository:r_17e70001"
	parityRepoRenamed    = "repository:r_2e4a0001"
	parityRepoBilling    = "repository:r_b1110001"
	parityRepoLedger     = "repository:r_1ed6e001"
	parityRepoOrders     = "repository:r_0dde5001"
	parityRepoGateway    = "repository:r_6a7e0001"
	parityRepoFluxSource = "repository:r_f1a50001"
	parityRepoExtControl = "repository:r_e87c0001"
	parityRepoExtConfig  = "repository:r_e87c0002"
	parityRepoSelfConfig = "repository:r_5e1f0001"
	parityRepoQuiet      = "repository:r_90000001"
	parityRepoLongOwner  = "repository:r_10e40001"
)

// parityNoiseRowsPerKind is how many content and file rows reference nothing
// owed. They carry a real repo_id, so a predicate that matches on the id
// namespace selects them. The file rows also carry the null argocd_* keys every
// parsed file has, which a marker on the key name selects.
const parityNoiseRowsPerKind = 20

// parityCatalog is the catalog the corpus resolves against. parityRepoRenamed
// has no slug alias and a remote that shares no token with its name, so only the
// RemoteURL can select the Flux fact that targets it.
func parityCatalog() []relationships.CatalogEntry {
	entry := func(repoID, name string) relationships.CatalogEntry {
		return relationships.CatalogEntry{
			RepoID:    repoID,
			Aliases:   []string{repoID, name, "acme/" + name},
			RemoteURL: "https://github.com/acme/" + name,
		}
	}
	return []relationships.CatalogEntry{
		entry(parityRepoPayments, "payments-api"),
		entry(parityRepoInventory, "inventory-api"),
		{
			RepoID:    parityRepoRenamed,
			Aliases:   []string{parityRepoRenamed, "local-checkout-name"},
			RemoteURL: "https://github.com/acme/renamed-config",
		},
		entry(parityRepoBilling, "billing-worker"),
		entry(parityRepoLedger, "ledger-worker"),
		entry(parityRepoOrders, "orders-worker"),
		entry(parityRepoGateway, "order-gateway"),
		entry(parityRepoFluxSource, "cluster-state"),
		entry(parityRepoExtControl, "control-plane"),
		entry(parityRepoExtConfig, "gitops-config"),
		entry(parityRepoSelfConfig, "gitops-monorepo"),
		entry(parityRepoQuiet, "quiet-service"),
		entry(parityRepoRouteTerraform, "route-terraform"),
		entry(parityRepoRouteTerragrunt, "route-terragrunt"),
		entry(parityRepoRouteHelmChart, "route-helm-chart"),
		entry(parityRepoRouteHelmValues, "route-helm-values"),
		entry(parityRepoRouteKustomize, "route-kustomize"),
		entry(parityRepoRouteDockerfile, "route-dockerfile"),
		entry(parityRepoRouteJenkins, "route-jenkins"),
		entry(parityRepoRouteArgoCDApp, "route-argocd-app"),
		entry(parityRepoRouteFluxAlias, "route-flux-alias"),
		entry(parityRepoRouteRawKind, "route-raw-kind"),
		entry(parityRepoMarkerSource, "marker-source"),
		entry(parityRepoFileConfig, "file-config"),
		entry(parityRepoEncodedSource, "encoded-source"),
		{
			RepoID:    parityRepoEncodedDash,
			Aliases:   []string{parityRepoEncodedDash, "local-dash-name"},
			RemoteURL: "https://github.com/acme/my-app",
		},
		{
			RepoID:    parityRepoEncodedAccent,
			Aliases:   []string{parityRepoEncodedAccent, "local-accent-name"},
			RemoteURL: "https://github.com/acme/café",
		},
		// The slug's owner is longer than its repository token, and the owner is in
		// the clone path of unrelated file rows, so a slug anchored on it loads
		// them. The last alias has no slash, so its longest token anchors it.
		{
			RepoID:    parityRepoLongOwner,
			Aliases:   []string{parityRepoLongOwner, "svc-api", "acme-corporation/svc-api", "orchestration core"},
			RemoteURL: "https://github.com/acme-corporation/svc-api",
		},
	}
}

// parityOwed is the set of repositories the pass owes.
func parityOwed() map[string]struct{} {
	return map[string]struct{}{
		parityRepoPayments:  {},
		parityRepoInventory: {},
		parityRepoRenamed:   {},

		parityRepoEncodedDash:   {},
		parityRepoEncodedAccent: {},
		parityRepoLongOwner:     {},
	}
}

// parityFact is one fact_records row in the corpus.
type parityFact struct {
	factID  string
	scopeID string
	kind    string
	payload map[string]any
}

func parityGeneration(scopeID string) string { return "gen-" + scopeID }

func parityContent(factID, repoID, path, artifactType, body string) parityFact {
	payload := map[string]any{"repo_id": repoID, "relative_path": path, "content": body}
	if artifactType != "" {
		payload["artifact_type"] = artifactType
	}
	return parityFact{factID: factID, scopeID: "git:" + repoID, kind: "content", payload: payload}
}

// parityCorpus holds one row per way evidence can reach an owed repository, plus
// noise rows that reference nothing owed. Each evidence row says which token it
// carries, because that is what the anchors have to find.
func parityCorpus() []parityFact {
	corpus := []parityFact{
		// Only an alias token of the owed repository.
		parityContent("alias-only", parityRepoBilling, "main.tf", "terraform",
			`app_repo = "payments-api"`),
		// A reference spelled as the whole slug still holds the repository token.
		parityContent("slug-only", parityRepoBilling, "slug.tf", "terraform",
			`app_repo = "acme-corporation/svc-api"`),
		// Only the full owed repo id, spelled as the id, no name or slug.
		parityContent("full-id-only", parityRepoLedger, "main.tf", "terraform",
			`app_repo = "`+parityRepoPayments+`"`),
		// Only the r_ token, held in the fact's own repo_id: the owed repository
		// is the SOURCE and the row names the target by alias.
		parityContent("r-token-only", parityRepoPayments, "main.tf", "terraform",
			`app_repo = "orders-worker"`),
		// A cloud scope with no repo_id at all.
		{
			factID: "gcp-inbound", scopeID: "gcp:project:demo", kind: cloud.GCPCloudRelationshipFactKind,
			payload: map[string]any{
				"source_full_resource_name": "//run.googleapis.com/projects/demo/services/order-gateway",
				"target_full_resource_name": "//secretmanager.googleapis.com/projects/demo/secrets/payments-api",
				"relationship_type":         "run_service_uses_secret",
				"support_state":             "supported",
			},
		},
		// Resolved by RemoteURL equality. The row holds no alias of the target.
		{
			factID: "flux-remote-only", scopeID: "git:" + parityRepoFluxSource, kind: "file",
			payload: map[string]any{
				"repo_id":       parityRepoFluxSource,
				"relative_path": "clusters/prod/source.yaml",
				"parsed_file_data": map[string]any{
					"flux_git_repositories": []any{map[string]any{
						"name": "platform", "namespace": "flux-system",
						"url": "ssh://git@github.com/Acme/Renamed-Config.git",
					}},
				},
			},
		},
		// The deploy repoURL is synthesized from an external config repository, so
		// no row holds payments-api.
		parityContent("appset-external", parityRepoExtControl, "appset.yaml", "argocd",
			parityAppSet("gitops-config")),
		parityContent("config-external", parityRepoExtConfig, "apps/payments/config.yaml", "",
			"team: payments\nservice: api\n"),
		// The generator reads config from the control repository itself (#7778),
		// and the deploy repoURL is synthesized from that file. No row holds
		// inventory-api, and the config row matches no anchor or ArgoCD marker.
		parityContent("appset-self", parityRepoSelfConfig, "appset.yaml", "argocd",
			parityAppSet("gitops-monorepo")),
		parityContent("config-self", parityRepoSelfConfig, "apps/inventory/config.yaml", "",
			"team: inventory\nservice: api\n"),
	}
	corpus = append(corpus, parityFileRouteRows()...)
	corpus = append(corpus, parityMarkerRows()...)
	corpus = append(corpus, parityNegativeRows()...)
	for index := range parityNoiseRowsPerKind {
		corpus = append(corpus,
			parityContent(
				fmt.Sprintf("noise-content-%d", index), parityRepoQuiet,
				fmt.Sprintf("src/noise-%d.tf", index), "terraform", `app_repo = "absent-service"`,
			),
			parityFact{
				factID: fmt.Sprintf("noise-file-%d", index), scopeID: "git:" + parityRepoQuiet, kind: "file",
				payload: map[string]any{
					"repo_id":       parityRepoQuiet,
					"relative_path": fmt.Sprintf("src/noise-%d.go", index),
					"parsed_file_data": map[string]any{
						"functions":              []any{},
						"argocd_applications":    nil,
						"argocd_applicationsets": nil,
					},
				},
			},
		)
	}
	return corpus
}

// parityAppSet is an ApplicationSet whose deploy repoURL exists only after
// rendering the generator's config file.
func parityAppSet(generatorRepoURL string) string {
	return "kind: ApplicationSet\n" +
		"spec:\n" +
		"  generators:\n" +
		"    - git:\n" +
		"        repoURL: " + generatorRepoURL + "\n" +
		"        files:\n" +
		"          - path: apps/*/config.yaml\n" +
		"  template:\n" +
		"    spec:\n" +
		"      source:\n" +
		"        repoURL: '{{.team}}-{{.service}}'\n"
}

// parityEnvelopes returns the corpus as the envelopes the fleet load would hand
// to discovery. Rows go through the same JSON decode the loaders use, so the two
// sides of the comparison cannot differ in payload shape.
func parityEnvelopes(t *testing.T, corpus []parityFact) []facts.Envelope {
	t.Helper()
	rows := &parityRows{t: t, facts: corpus}
	envelopes := make([]facts.Envelope, 0, len(corpus))
	for rows.Next() {
		envelope, err := backfill.ScanFactEnvelope(rows)
		if err != nil {
			t.Fatalf("scan corpus fact: %v", err)
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

// parityQueryer answers the two read statements the anchor-scoped load issues by
// evaluating their predicates over the corpus. A statement it does not recognize
// fails the test, so the harness cannot quietly drop a new read. It records the
// row counts so a test can show what each predicate returned.
type parityQueryer struct {
	t      *testing.T
	corpus []parityFact

	anchorRows int
	configRows int
}

func (q *parityQueryer) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	q.t.Helper()
	if len(args) != 1 {
		q.t.Fatalf("query takes %d args, want 1", len(args))
	}
	values, ok := args[0].(array.StringArray)
	if !ok {
		q.t.Fatalf("query arg is %T, want array.StringArray", args[0])
	}
	var matched []parityFact
	switch query {
	case backfill.ListOnboardedRepoScopedRelationshipFactRecordsQuery:
		matched = q.matchAnchors(values)
		q.anchorRows += len(matched)
	case backfill.ListArgoCDGeneratorConfigFactRecordsQuery:
		matched = q.matchConfig(values)
		q.configRows += len(matched)
	default:
		q.t.Fatalf("unexpected statement:\n%s", query)
	}
	return &parityRows{t: q.t, facts: matched}, nil
}

// ExecContext satisfies db.ExecQueryer. The read path issues no writes.
func (q *parityQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	q.t.Fatal("the anchor-scoped read path must not write")
	return nil, nil
}

// matchAnchors evaluates lower(fact.payload::text) LIKE ANY($1), honoring the
// LIKE escaping so a term such as %r\_9a7e0001% matches a literal underscore.
func (q *parityQueryer) matchAnchors(terms []string) []parityFact {
	var matched []parityFact
	for _, fact := range q.corpus {
		text := strings.ToLower(q.payloadText(fact))
		for _, term := range terms {
			if strings.Contains(text, parityLikeLiteral(term)) {
				matched = append(matched, fact)
				break
			}
		}
	}
	return matched
}

// matchConfig evaluates the repo_id and path filter of the config statement.
func (q *parityQueryer) matchConfig(repoIDs []string) []parityFact {
	wanted := make(map[string]struct{}, len(repoIDs))
	for _, repoID := range repoIDs {
		wanted[repoID] = struct{}{}
	}
	var matched []parityFact
	for _, fact := range q.corpus {
		repoID, _ := fact.payload["repo_id"].(string)
		if _, ok := wanted[repoID]; !ok || (fact.kind != "content" && fact.kind != "file") {
			continue
		}
		path, _ := fact.payload["relative_path"].(string)
		path = strings.ToLower(path)
		if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".json") {
			matched = append(matched, fact)
		}
	}
	return matched
}

func (q *parityQueryer) payloadText(fact parityFact) string {
	q.t.Helper()
	raw, err := json.Marshal(fact.payload)
	if err != nil {
		q.t.Fatalf("marshal %s: %v", fact.factID, err)
	}
	return parityJSONBText(raw)
}

// parityJSONBText renders compact JSON the way Postgres prints jsonb, with a
// space after every colon and comma outside a string. Spelling is part of the
// predicate, so the harness has to test the text the statement tests;
// TestAnchorScopedLoadKeepsOwedEvidenceOnPostgres holds it to a real server.
func parityJSONBText(compact []byte) string {
	var text strings.Builder
	inString, escaped := false, false
	for _, char := range string(compact) {
		text.WriteRune(char)
		switch {
		case escaped:
			escaped = false
		case inString && char == '\\':
			escaped = true
		case char == '"':
			inString = !inString
		case !inString && (char == ':' || char == ','):
			text.WriteByte(' ')
		}
	}
	return text.String()
}

// parityLikeLiteral strips the %...% wrapper and the backslash escapes.
func parityLikeLiteral(term string) string {
	term = strings.TrimSuffix(strings.TrimPrefix(term, "%"), "%")
	return strings.NewReplacer(`\\`, `\`, `\%`, `%`, `\_`, `_`).Replace(term)
}

// parityRows serves corpus facts in the 16-column order ScanFactEnvelope reads.
type parityRows struct {
	t     *testing.T
	facts []parityFact
	next  int
}

func (r *parityRows) Next() bool   { r.next++; return r.next <= len(r.facts) }
func (r *parityRows) Err() error   { return nil }
func (r *parityRows) Close() error { return nil }

func (r *parityRows) Scan(dest ...any) error {
	r.t.Helper()
	fact := r.facts[r.next-1]
	raw, err := json.Marshal(fact.payload)
	if err != nil {
		return err
	}
	schemaVersion := "1.0.0"
	if fact.kind == cloud.GCPCloudRelationshipFactKind {
		schemaVersion = cloud.GCPCloudRelationshipSchemaVersion
	}
	values := []any{
		fact.factID, fact.scopeID, parityGeneration(fact.scopeID), fact.kind,
		"stable-" + fact.factID, schemaVersion, "git", int64(1), "high", "git",
		"src-" + fact.factID, "", "", time.Unix(0, 0).UTC(), false, raw,
	}
	if len(dest) != len(values) {
		return fmt.Errorf("scan into %d columns, want %d", len(dest), len(values))
	}
	for index, value := range values {
		switch target := dest[index].(type) {
		case *string:
			*target = value.(string)
		case *int64:
			*target = value.(int64)
		case *bool:
			*target = value.(bool)
		case *time.Time:
			*target = value.(time.Time)
		case *[]byte:
			*target = value.([]byte)
		default:
			return fmt.Errorf("column %d: unsupported scan target %T", index, dest[index])
		}
	}
	return nil
}

// ParityRow is one corpus row in the shape fact_records stores, for the external
// live proof. That proof has to run from targeted_test, which cannot read this
// package's unexported fixtures.
type ParityRow struct {
	FactID, StableFactKey, ScopeID, GenerationID, Kind, SchemaVersion string
	Payload                                                           []byte
}

// ParityRows returns the corpus for the external live proof.
func ParityRows(t *testing.T) []ParityRow {
	t.Helper()
	corpus := parityCorpus()
	rows := make([]ParityRow, 0, len(corpus)+2)
	for _, fact := range corpus {
		raw, err := json.Marshal(fact.payload)
		if err != nil {
			t.Fatalf("marshal %s: %v", fact.factID, err)
		}
		schemaVersion := "1.0.0"
		if fact.kind == cloud.GCPCloudRelationshipFactKind {
			schemaVersion = cloud.GCPCloudRelationshipSchemaVersion
		}
		rows = append(rows, ParityRow{
			FactID: fact.factID, StableFactKey: "stable-" + fact.factID, ScopeID: fact.scopeID,
			GenerationID: parityGeneration(fact.scopeID), Kind: fact.kind, SchemaVersion: schemaVersion, Payload: raw,
		})
	}
	return append(rows, parityWhitespaceRows()...)
}

// ParityCatalog returns the corpus catalog for the external live proof.
func ParityCatalog() []relationships.CatalogEntry { return parityCatalog() }

// ParityOwed returns the owed repositories for the external live proof.
func ParityOwed() map[string]struct{} { return parityOwed() }

// ParityTouchingOwed projects evidence the way the parity tests compare it.
func ParityTouchingOwed(evidence []relationships.EvidenceFact, owed map[string]struct{}) []string {
	return touchingOwed(evidence, owed)
}

// ParityMissing returns the rows of want that got lacks.
func ParityMissing(want, got []string) []string { return missingRows(want, got) }

// ParityFleetEvidence returns the reference answer over every corpus row.
func ParityFleetEvidence(t *testing.T, catalog []relationships.CatalogEntry, owed map[string]struct{}) []string {
	t.Helper()
	return fleetEvidence(t, catalog, owed)
}
