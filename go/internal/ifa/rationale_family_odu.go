// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// The rationale_edges family Odù (#5998, under the #5543 umbrella).
//
// rationale.ExtractRows projects one Rationale node and one EXPLAINS edge
// per distinct (entity, comment kind, comment text) carried in a content
// entity's parser-emitted rationale_comments. The fixture below exercises every
// clause that decides whether an edge exists, because an Odù carrying only
// well-formed comments proves the extractor emits edges without proving it
// emits the right ones — and the set is asserted exactly, so an edge nobody
// derived fails as loudly as a missing one.
// Every constant below that the moved guard needs is exported (#6053): the rationale_edges guard and its
// two live-fixture tests (rationale_family_live_fixture_test.go,
// rationale_family_delta_live_fixture_test.go) moved to materializededges
// because they exercise the moved guard, and that package can only rebuild
// this Odù's exact facts -- entity IDs, scope/generation coordinates, source
// paths -- by reading these identifiers from here, not a second copy of them.
const (
	// RationaleFamilyOduName is this Odù's catalog name.
	RationaleFamilyOduName = "odu:ifa-rationale-family"
	// RationaleFamilyScopeID is the ScopeID every fact in this Odù carries.
	RationaleFamilyScopeID = "scope-ifa-rationale-family"
	// RationaleFamilyGenerationID is the gen-1 GenerationID every base fact in
	// this Odù carries.
	RationaleFamilyGenerationID = "gen-ifa-rationale-family-1"
	// RationaleFamilyDeltaGenerationID is the gen-2 GenerationID the delta
	// live-fixture test stamps onto its own facts, proving the reducer
	// re-derives rationale edges after a second generation for the same repo.
	RationaleFamilyDeltaGenerationID = "gen-ifa-rationale-family-2"
	// RationaleFamilyRepoID is this Odù's repository ID.
	RationaleFamilyRepoID    = "repository:r_f781caa5"
	rationaleFamilyRemoteURL = "https://github.com/eshu-hq/ifa-rationale-fixture"
	// RationaleFamilyCassetteRelPath is the repo-root-relative path to this
	// family's committed cassette.
	RationaleFamilyCassetteRelPath = "testdata/cassettes/rationale/ifa-rationale-family.json"
	// RationaleFamilyDeltaCassetteRelPath is the repo-root-relative path to this
	// family's delta cassette. Exported for the same reason as the baseline path
	// above: two tests in different packages need it, and a copy in either is
	// free to drift into a path that still exists, which no assertion catches.
	RationaleFamilyDeltaCassetteRelPath = "testdata/cassettes/rationale/ifa-rationale-family-delta.json"
	// RationaleFamilySourceRunID is the collector SourceRunID stamped onto
	// this Odù's repository fact.
	RationaleFamilySourceRunID = "run-ifa-rationale-family-1"
	// RationaleFamilyLocalPath is the repository's checkout path every file
	// and content-entity fact's paths are anchored under.
	RationaleFamilyLocalPath = "/repo-rationale"
	// RationaleFamilyChargePath and RationaleFamilyInvoicePath are the two
	// Python files whose rationale comments derive EXPLAINS edges (charge
	// derives WHY edges with a duplicate and an empty TODO exercising
	// dedup/rejection; invoice derives one HACK edge).
	RationaleFamilyChargePath  = "services/payments/charge.py"
	RationaleFamilyInvoicePath = "services/billing/invoice.py"
	// RationaleFamilyRefundPath, RationaleFamilyReconcilePath, and
	// RationaleFamilyHealthcheckPath are the negative-case Python files this
	// Odù carries (no rationale comments, or comments the parser does not
	// recognize).
	RationaleFamilyRefundPath      = "services/payments/refund.py"
	RationaleFamilyReconcilePath   = "services/support/reconcile.py"
	RationaleFamilyHealthcheckPath = "services/support/healthcheck.py"
	// RationaleFamilyChargeName and RationaleFamilyInvoiceName are the
	// top-level function names content.CanonicalEntityID keys on.
	RationaleFamilyChargeName  = "charge"
	RationaleFamilyInvoiceName = "issue_invoice"
	// RationaleFamilyChargeLine and RationaleFamilyInvoiceLine are the
	// declaration line numbers content.CanonicalEntityID keys on.
	RationaleFamilyChargeLine  = 5
	RationaleFamilyInvoiceLine = 2
)

// rationaleFamilyOdu carries five parser-reachable Python functions. Two
// functions derive exactly three EXPLAINS edges; the other three pin the
// parser's case, supported-marker, adjacency, and no-comment exclusions. The
// charge function repeats WHY and carries an empty TODO so exact-set comparison
// also pins deduplication and empty-text rejection. Synthetic malformed-envelope
// and precedence guards live in reducer unit tests rather than this live Odù.
func rationaleFamilyOdu() CatalogOdu {
	const (
		chargeText  = "Retries are capped at three to bound tail latency."
		invoiceText = "Invoices are immutable once issued."
	)
	chargeID := content.CanonicalEntityID(
		RationaleFamilyRepoID, RationaleFamilyChargePath, "Function",
		RationaleFamilyChargeName, RationaleFamilyChargeLine,
	)
	invoiceID := content.CanonicalEntityID(
		RationaleFamilyRepoID, RationaleFamilyInvoicePath, "Function",
		RationaleFamilyInvoiceName, RationaleFamilyInvoiceLine,
	)
	refundID := content.CanonicalEntityID(RationaleFamilyRepoID, RationaleFamilyRefundPath, "Function", "refund", 3)
	reconcileID := content.CanonicalEntityID(RationaleFamilyRepoID, RationaleFamilyReconcilePath, "Function", "reconcile", 3)
	healthID := content.CanonicalEntityID(RationaleFamilyRepoID, RationaleFamilyHealthcheckPath, "Function", "healthcheck", 1)
	sourceRunID := RationaleFamilySourceRunID
	localPath := RationaleFamilyLocalPath
	files := []struct {
		path         string
		name         string
		entityID     string
		startLine    int
		endLine      int
		entitySource string
		comments     []any
	}{
		{RationaleFamilyInvoicePath, RationaleFamilyInvoiceName, invoiceID, RationaleFamilyInvoiceLine, 3, "def issue_invoice():\n    pass\n", []any{
			map[string]any{"kind": "HACK", "text": invoiceText},
		}},
		{RationaleFamilyChargePath, RationaleFamilyChargeName, chargeID, RationaleFamilyChargeLine, 6, "def charge():\n    pass\n", []any{
			map[string]any{"kind": "WHY", "text": chargeText},
			map[string]any{"kind": "NOTE", "text": chargeText},
			map[string]any{"kind": "WHY", "text": chargeText},
			map[string]any{"kind": "TODO", "text": ""},
		}},
		{RationaleFamilyRefundPath, "refund", refundID, 3, 4, "def refund():\n    pass\n", nil},
		{RationaleFamilyHealthcheckPath, "healthcheck", healthID, 1, 2, "def healthcheck():\n    pass\n", nil},
		{RationaleFamilyReconcilePath, "reconcile", reconcileID, 3, 4, "def reconcile():\n    pass\n", nil},
	}
	factsForOdu := make([]facts.Envelope, 0, 12)
	graphID, graphKind, repoName, parsedCount := RationaleFamilyRepoID, "repository", "repo-rationale", "5"
	repoSlug, remoteURL := "eshu-hq/ifa-rationale-fixture", rationaleFamilyRemoteURL
	isDependency := false
	factsForOdu = append(factsForOdu, RationaleFamilyRepositoryFact(codegraphv1.Repository{
		RepoID: RationaleFamilyRepoID, SourceRunID: &sourceRunID, LocalPath: &localPath,
		GraphID: &graphID, GraphKind: &graphKind, Name: &repoName,
		ParsedFileCount: &parsedCount, IsDependency: &isDependency,
		RepoSlug: &repoSlug, RemoteURL: &remoteURL,
	}))
	for _, file := range files {
		fileGraphID, fileGraphKind, language := RationaleFamilyRepoID+":"+file.path, "file", "python"
		factsForOdu = append(factsForOdu, RationaleFamilyFileFact(codegraphv1.File{
			RepoID: RationaleFamilyRepoID, RelativePath: file.path,
			ParsedFileData: rationaleFamilyParsedFile(file.path, file.name, file.entityID, file.startLine, file.endLine, file.entitySource, file.comments),
			GraphID:        &fileGraphID, GraphKind: &fileGraphKind,
			IsDependency: &isDependency, Language: &language,
		}))
	}
	for _, file := range files {
		metadata := map[string]any{
			"args": nil, "async": false, "cyclomatic_complexity": float64(1), "decorators": nil,
		}
		if len(file.comments) > 0 {
			metadata["rationale_comments"] = file.comments
		}
		factsForOdu = append(factsForOdu, rationaleFamilyContentEntity(
			file.entityID, file.name, file.path, file.startLine, file.endLine, file.entitySource, metadata,
		))
	}
	factsForOdu = append(factsForOdu, rationaleFamilyFollowupFact())
	odu := Odu{
		Name:  RationaleFamilyOduName,
		Facts: factsForOdu,
	}
	return CatalogOdu{
		Odu: odu,
		Detail: "one typed repository, five typed Python files, five valid content entities, and the production rationale follow-up; " +
			"uppercase parser comments derive exactly three EXPLAINS edges while duplicate WHY, empty TODO, lowercase why, unsupported CAVEAT, " +
			"blank-line-detached FIXME, and a no-comment function derive none",
	}
}

// rationaleFamilyContentEntity builds a live Function content_entity envelope
// carrying the given payload fields alongside this fixture's canonical inputs.
func rationaleFamilyContentEntity(entityID, name, relativePath string, startLine, endLine int, source string, metadata map[string]any) facts.Envelope {
	payload := map[string]any{
		"graph_id":      entityID,
		"graph_kind":    "content_entity",
		"repo_id":       RationaleFamilyRepoID,
		"entity_id":     entityID,
		"entity_type":   "Function",
		"entity_name":   name,
		"relative_path": relativePath,
		"start_line":    float64(startLine),
		"end_line":      float64(endLine),
		"language":      "python",
		"source_cache":  source,
		"indexed_at":    "1970-01-01T00:00:00Z",
		"iac_relevant":  false,
	}
	if len(metadata) > 0 {
		payload["entity_metadata"] = metadata
	}
	return rationaleFamilyFact("content_entity", "content_entity:"+entityID, payload, false)
}

// RationaleFamilyRepositoryFact builds one typed "repository" fact from
// repository, stamped with this family's scope/generation coordinates and
// carrying the fixture's imports_map. Exported (#6053) so materializededges'
// moved rationale-family live-fixture tests can build the same repository
// fact this Odù's own builder uses, rather than a second, driftable copy.
func RationaleFamilyRepositoryFact(repository codegraphv1.Repository) facts.Envelope {
	payload, err := factschema.EncodeCodegraphRepository(repository)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode rationale repository %q: %v", repository.RepoID, err))
	}
	payload["imports_map"] = map[string]any{
		"charge":        []any{RationaleFamilyLocalPath + "/" + RationaleFamilyChargePath},
		"healthcheck":   []any{RationaleFamilyLocalPath + "/" + RationaleFamilyHealthcheckPath},
		"issue_invoice": []any{RationaleFamilyLocalPath + "/" + RationaleFamilyInvoicePath},
		"reconcile":     []any{RationaleFamilyLocalPath + "/" + RationaleFamilyReconcilePath},
		"refund":        []any{RationaleFamilyLocalPath + "/" + RationaleFamilyRefundPath},
	}
	return rationaleFamilyFact(factschema.FactKindCodegraphRepository, "repository:"+repository.RepoID, payload, false)
}

// RationaleFamilyFileFact builds one typed "file" fact from file, stamped
// with this family's scope/generation coordinates. Exported (#6053) for the
// same reason as RationaleFamilyRepositoryFact.
func RationaleFamilyFileFact(file codegraphv1.File) facts.Envelope {
	payload, err := factschema.EncodeCodegraphFile(file)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode rationale file %q: %v", file.RelativePath, err))
	}
	return rationaleFamilyFact(factschema.FactKindCodegraphFile, "file:"+file.RepoID+":"+file.RelativePath, payload, false)
}

func rationaleFamilyParsedFile(relativePath, name, uid string, startLine, endLine int, source string, comments []any) map[string]any {
	function := map[string]any{
		"args": []any{}, "async": false, "cyclomatic_complexity": float64(1), "decorators": []any{},
		"name": name, "uid": uid, "line_number": float64(startLine), "end_line": float64(endLine),
		"lang": "python", "source": strings.TrimSuffix(source, "\n"),
	}
	if comments != nil {
		function["rationale_comments"] = comments
	}
	parsed := map[string]any{
		"path":          RationaleFamilyLocalPath + "/" + relativePath,
		"repo_path":     RationaleFamilyLocalPath,
		"lang":          "python",
		"is_dependency": false,
		"iac_relevant":  false,
		"functions":     []any{function},
		"classes":       []any{}, "embedded_shell_commands": []any{}, "function_calls": []any{},
		"imports": []any{}, "modules": []any{}, "orm_table_mappings": []any{},
		"type_annotations": []any{}, "variables": []any{},
		"framework_semantics": map[string]any{"frameworks": []any{}},
	}
	for _, key := range rationaleFamilyNilParserKeys {
		parsed[key] = nil
	}
	return parsed
}

var rationaleFamilyNilParserKeys = []string{
	"analytics_models", "annotations", "argocd_applications", "argocd_applicationsets",
	"atlantis_projects", "atlantis_workflows", "cloudformation_conditions",
	"cloudformation_cross_stack_exports", "cloudformation_cross_stack_imports",
	"cloudformation_outputs", "cloudformation_parameters", "cloudformation_resources",
	"components", "crossplane_claims", "crossplane_compositions", "crossplane_xrds",
	"dashboard_assets", "data_assets", "data_columns", "data_contracts", "data_owners",
	"data_quality_checks", "enums", "flux_buckets", "flux_git_repositories",
	"flux_helm_releases", "flux_helm_repositories", "flux_kustomizations",
	"flux_oci_repositories", "gitlab_jobs", "gitlab_pipelines", "helm_charts",
	"helm_template_value_usages", "helm_value_definitions", "helm_values", "impl_blocks",
	"interfaces", "k8s_resources", "kustomize_overlays", "macros", "pagerduty_declarations",
	"properties", "protocol_implementations", "protocols", "query_executions", "records",
	"sql_columns", "sql_functions", "sql_indexes", "sql_migrations", "sql_tables",
	"sql_triggers", "sql_views", "structs", "terraform_backends", "terraform_blocks",
	"terraform_checks", "terraform_data_sources", "terraform_imports", "terraform_locals",
	"terraform_lock_providers", "terraform_modules", "terraform_moved_blocks",
	"terraform_outputs", "terraform_providers", "terraform_removed_blocks",
	"terraform_resources", "terraform_variables", "terragrunt_configs",
	"terragrunt_dependencies", "terragrunt_inputs", "terragrunt_locals", "traits",
	"type_aliases", "typedefs", "unions",
}

func rationaleFamilyFollowupFact() facts.Envelope {
	return rationaleFamilyFact("shared_followup", "shared_followup:"+RationaleFamilyRepoID+":rationale_materialization", map[string]any{
		"reducer_domain": "rationale_materialization",
		"entity_key":     "rationale:repo-rationale",
		"reason":         "repository generation requested rationale materialization reconciliation",
		"repo_id":        RationaleFamilyRepoID,
	}, false)
}

func rationaleFamilyFact(factKind, stableFactKey string, payload map[string]any, tombstone bool) facts.Envelope {
	return facts.Envelope{
		ScopeID:          RationaleFamilyScopeID,
		GenerationID:     RationaleFamilyGenerationID,
		FactKind:         factKind,
		StableFactKey:    stableFactKey,
		SchemaVersion:    "1.0.0",
		CollectorKind:    "git",
		SourceConfidence: "observed",
		IsTombstone:      tombstone,
		Payload:          payload,
	}
}
