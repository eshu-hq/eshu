// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

const (
	scimPackage  = "npm://registry.npmjs.org/axios"
	scimPURL     = "pkg:npm/axios"
	scimCVE      = "CVE-2026-1000"
	scimAdvisory = "GHSA-aaaa-bbbb-cccc"
	scimCriteria = "cpe:2.3:a:axios:axios:*:*:*:*:*:node.js:*:*"
	scimDocument = "sbom-document-1"
	scimImage    = "registry.example/team/api:prod"
	// scimCVESuppressions is how many suppressions match scimCVE.
	scimCVESuppressions = 80 + 3
	// scimBulkCore pushes core evidence across the 500-row page boundary.
	scimBulkCore = 650
)

var (
	scimDigest       = "sha256:" + strings.Repeat("a", 64)
	scimRepositories = []string{"repository:m:r1", "repository:m:r2", "repository:m:r3", "repository:m:r4"}
)

// supplyChainImpactMatrixAllFilter binds every identity the matrix carries.
func supplyChainImpactMatrixAllFilter() supplychaincore.SupplyChainImpactFactFilter {
	return supplychaincore.SupplyChainImpactFactFilter{
		PackageIDs: []string{scimPackage}, PURLs: []string{scimPURL},
		CVEIDs: []string{scimCVE}, AdvisoryIDs: []string{scimAdvisory},
		SubjectDigests: []string{scimDigest}, ProductCriteria: []string{scimCriteria},
		DocumentIDs: []string{scimDocument}, RepositoryIDs: scimRepositories,
		FileRepositoryIDs: scimRepositories, ImageRefs: []string{scimImage},
	}
}

// seedSupplyChainImpactMatrix seeds the ruling 4.2 matrix: every arm of every
// scanned kind, the lifecycle edges (tombstone, superseded and non-active
// generations), suppression normalization cases, deployment-only
// suppressions, a fact matching two keys, foreign keys on growth-class kinds
// and on files, page-boundary core rows, a suppression set larger than the
// lowered cap, and a two-page identity stream.
func seedSupplyChainImpactMatrix(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	scope := func(id, sourceKey, payload, active string, extra map[string]string) {
		supplyChainImpactFixtureScope(t, ctx, database, id, sourceKey, payload, active, extra)
	}
	fact := func(id, scopeID, generation, kind, payload string) {
		supplyChainImpactFixtureFact(t, ctx, database, id, scopeID, generation, kind, payload, false)
	}
	j := func(pairs ...string) string {
		parts := make([]string, 0, len(pairs)/2)
		for i := 0; i+1 < len(pairs); i += 2 {
			parts = append(parts, fmt.Sprintf("%q: %s", pairs[i], pairs[i+1]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	q := func(value string) string { return fmt.Sprintf("%q", value) }

	scope("osv:m", "osv:m", `{}`, "gen:osv:m", map[string]string{"gen:osv:m:old": "superseded"})
	scope("osv:pending", "osv:pending", `{}`, "gen:osv:pending", nil)
	if _, err := database.ExecContext(ctx, `UPDATE scope_generations SET status = 'pending' WHERE generation_id = 'gen:osv:pending'`); err != nil {
		t.Fatalf("mark pending generation: %v", err)
	}
	scope("registry:m", "registry:m", `{}`, "gen:registry:m", nil)
	scope("sbom:m", "sbom:m", `{}`, "gen:sbom:m", nil)
	scope("vex:m", "vex:m", `{}`, "gen:vex:m", nil)
	scope("git:m:r1", "repository:m:r1", `{"repo_id": "repository:m:r1"}`, "gen:r1", nil)
	scope("git:m:r2", "src:m:r2", `{"id": "repository:m:r2"}`, "gen:r2", nil)
	scope("repository:m:r3", "src:m:r3", `{}`, "gen:r3", nil)
	scope("git:m:r4", "repository:m:r4", `{}`, "gen:r4", nil)
	scope("git:m:r5", "repository:m:r5", `{"repo_id": "repository:m:r5"}`, "gen:r5", nil)

	// Vulnerability catalog (growth class, key probes).
	fact("m:ap:two-keys", "osv:m", "gen:osv:m", "vulnerability.affected_package", j("package_id", q(scimPackage), "purl", q(scimPURL)))
	fact("m:ap:cve", "osv:m", "gen:osv:m", "vulnerability.affected_package", j("cve_id", q(scimCVE)))
	fact("m:ap:adv", "osv:m", "gen:osv:m", "vulnerability.affected_package", j("advisory_id", q(scimAdvisory)))
	fact("m:cve:1", "osv:m", "gen:osv:m", "vulnerability.cve", j("cve_id", q(scimCVE), "advisory_id", q(scimAdvisory)))
	fact("m:cve:foreign-package", "osv:m", "gen:osv:m", "vulnerability.cve", j("cve_id", q("CVE-2026-9999"), "package_id", q(scimPackage)))
	fact("m:prod:criteria", "osv:m", "gen:osv:m", "vulnerability.affected_product", j("cve_id", q("CVE-2026-8888"), "criteria", q(scimCriteria)))
	fact("m:prod:cve", "osv:m", "gen:osv:m", "vulnerability.affected_product", j("cve_id", q(scimCVE), "criteria", q("cpe:other")))
	fact("m:epss:cve", "osv:m", "gen:osv:m", "vulnerability.epss_score", j("cve_id", q(scimCVE)))
	fact("m:kev:cve", "osv:m", "gen:osv:m", "vulnerability.known_exploited", j("cve_id", q(scimCVE)))
	fact("m:kev:adv", "osv:m", "gen:osv:m", "vulnerability.known_exploited", j("cve_id", q("CVE-2026-7777"), "advisory_id", q(scimAdvisory)))
	supplyChainImpactFixtureFact(t, ctx, database, "m:ap:tombstone", "osv:m", "gen:osv:m", "vulnerability.affected_package", j("package_id", q(scimPackage)), true)
	fact("m:ap:old-generation", "osv:m", "gen:osv:m:old", "vulnerability.affected_package", j("package_id", q(scimPackage)))
	fact("m:ap:pending", "osv:pending", "gen:osv:pending", "vulnerability.affected_package", j("package_id", q(scimPackage)))
	for n := 1; n <= scimBulkCore; n++ {
		fact(fmt.Sprintf("m:ap:bulk:%04d", n), "osv:m", "gen:osv:m", "vulnerability.affected_package", j("package_id", q(scimPackage), "cve_id", q(fmt.Sprintf("CVE-2026-B%04d", n))))
	}

	// Registry, alert, OCI, and SBOM kinds (growth class).
	for id, row := range map[string][2]string{
		"m:alert:package":              {"security_alert.repository_alert", j("package_id", q(scimPackage), "repository_id", q("repository:m:r5"))},
		"m:alert:cve":                  {"security_alert.repository_alert", j("cve_id", q(scimCVE))},
		"m:pv:package":                 {"package_registry.package_version", j("package_id", q(scimPackage))},
		"m:pv:purl":                    {"package_registry.package_version", j("purl", q(scimPURL))},
		"m:hint:package":               {"package_registry.vulnerability_hint", j("package_id", q(scimPackage))},
		"m:hint:adv":                   {"package_registry.vulnerability_hint", j("advisory_id", q(scimAdvisory))},
		"m:oci:manifest":               {"oci_registry.image_manifest", j("digest", q(scimDigest), "repository_id", q("repository:m:r1"))},
		"m:oci:index":                  {"oci_registry.image_index", j("digest", q(scimDigest))},
		"m:oci:tag":                    {"oci_registry.image_tag_observation", j("resolved_digest", q(scimDigest))},
		"m:oci:referrer:subject":       {"oci_registry.image_referrer", j("subject_digest", q(scimDigest))},
		"m:oci:referrer:referrer":      {"oci_registry.image_referrer", j("referrer_digest", q(scimDigest))},
		"m:oci:manifest:foreign-image": {"oci_registry.image_manifest", j("digest", q("sha256:other"), "image_ref", q(scimImage))},
	} {
		fact(id, "registry:m", "gen:registry:m", row[0], row[1])
	}
	for id, row := range map[string][2]string{
		"m:sbom:package":    {"sbom.component", j("package_id", q(scimPackage))},
		"m:sbom:purl":       {"sbom.component", j("purl", q(scimPURL))},
		"m:sbom:cpe":        {"sbom.component", j("cpe", q(scimCriteria))},
		"m:sbom:document":   {"sbom.component", j("document_id", q(scimDocument), "scope", q("required"))},
		"m:attach:subject":  {"reducer_sbom_attestation_attachment", j("subject_digest", q(scimDigest), "scope_id", q("sbom:m"))},
		"m:attach:document": {"reducer_sbom_attestation_attachment", j("document_id", q(scimDocument))},
	} {
		fact(id, "sbom:m", "gen:sbom:m", row[0], row[1])
	}

	// Suppressions (bounded class): normalization, deployment-only, cap set.
	for id, payload := range map[string]string{
		"m:supp:cve-lower":   j("scope", j("cve_id", q("cve-2026-1000"))),
		"m:supp:purl-pad":    j("scope", j("purl", q("  pkg:npm/AXIOS\t"))),
		"m:supp:pkg-unicode": j("scope", j("package_id", q("　npm://registry.npmjs.org/Axios "))),
		"m:supp:adv":         j("scope", j("advisory_id", q(" ghsa-AAAA-bbbb-cccc "))),
		"m:supp:repo":        j("scope", j("repository_id", q(" REPOSITORY:M:R1 "))),
		"m:supp:subject":     j("scope", j("subject_digest", q(strings.ToUpper(scimDigest)))),
		"m:supp:top-cve":     j("cve_id", q(scimCVE)),
		"m:supp:deploy-only": j("scope", j("environment", q("prod"), "workload_id", q("workload:api"), "service_id", q("service:api"))),
	} {
		fact(id, "vex:m", "gen:vex:m", "vulnerability.suppression", payload)
	}
	for n := 1; n <= 80; n++ {
		fact(fmt.Sprintf("m:supp:bulk:%03d", n), "vex:m", "gen:vex:m", "vulnerability.suppression", j("scope", j("cve_id", q(scimCVE))))
	}

	// Repository scopes: reducer kinds (both classes) and parser files.
	jsts := func(repo, lang string) string {
		return j("repo_id", q(repo), "relative_path", q("src/index."+lang), "parsed_file_data", j("language", q(lang)))
	}
	for _, row := range [][4]string{
		{"m:plat:r1:scope", "git:m:r1", "reducer_platform_materialization", j("scope_id", q("git:m:r1"))},
		{"m:ci:r1:repository", "git:m:r1", "reducer_ci_cd_run_correlation", j("repository_id", q("repository:m:r1"), "artifact_digest", q(scimDigest), "image_ref", q(scimImage), "scope_id", q("git:m:r1"))},
		{"m:cons:r1:package", "git:m:r1", "reducer_package_consumption_correlation", j("package_id", q(scimPackage), "repository_id", q("repository:m:r1"))},
		{"m:file:r1:ts", "git:m:r1", "file", jsts("repository:m:r1", "typescript")},
		{"m:file:r1:go", "git:m:r1", "file", jsts("repository:m:r1", "go")},
		{"m:file:r1:jsx-upper", "git:m:r1", "file", j("repo_id", q("repository:m:r1"), "language", q("JSX"))},
		{"m:wl:r2:payload-id", "git:m:r2", "reducer_workload_identity", j("scope_id", q("git:m:r2"))},
		{"m:file:r2:scope-only", "git:m:r2", "file", jsts("repository:other", "javascript")},
		{"m:svc:r3:scope-id", "repository:m:r3", "reducer_service_catalog_correlation", j("repository_id", q("repository:other"))},
		{"m:cons:r3:payload-scope", "repository:m:r3", "reducer_package_consumption_correlation", j("scope_id", q("repository:m:r3"))},
		{"m:ci:r4:source-key", "git:m:r4", "reducer_ci_cd_run_correlation", j("repository_id", q("repository:other"))},
		{"m:plat:r4:image-ref", "git:m:r4", "reducer_platform_materialization", j("image_ref", q(scimImage))},
		{"m:file:r4:tsx-scope-key", "git:m:r4", "file", jsts("repository:other", "tsx")},
		{"m:ci:r5", "git:m:r5", "reducer_ci_cd_run_correlation", j("repository_id", q("repository:m:r5"))},
		{"m:file:r5:ts", "git:m:r5", "file", jsts("repository:m:r5", "typescript")},
		{"m:file:r5:foreign-cve", "git:m:r5", "file", j("repo_id", q("repository:m:r5"), "cve_id", q(scimCVE))},
	} {
		fact(row[0], row[1], "gen:"+strings.TrimPrefix(strings.TrimPrefix(row[1], "git:m:"), "repository:m:"), row[2], row[3])
	}

	supplyChainImpactFixtureIdentity(t, ctx, database, "osv:m", "gen:osv:m", 620)
	supplyChainImpactFixtureAnalyze(t, ctx, database)
}
