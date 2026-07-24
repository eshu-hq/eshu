// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/mcp"
)

const (
	defaultSpecsDir    = "../specs"
	defaultDocsDir     = "../docs/public"
	defaultRoot        = ".."
	defaultArtifactOut = "internal/capabilitycatalog/data/catalog.generated.json"
	envVerifyIssues    = "ESHU_VERIFY_PRODUCT_CLAIM_ISSUES_LIVE"
	// #nosec G101 -- this is the public environment variable name; the token value is read at runtime.
	envGitHubToken = "GITHUB_TOKEN"
	// defaultRemoteValidationBaseline is the remote-validation mode's default
	// burn-down baseline path, relative to the process's working directory
	// (go/cmd/capability-inventory), matching defaultSpecsDir's convention.
	defaultRemoteValidationBaseline = "../specs/remote-validation-baseline.txt"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run executes the capability-inventory command. modes: report (default) prints
// the catalog and findings; generate writes the artifact; verify fails when the
// catalog has findings or the committed artifact is stale.
func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("capability-inventory", flag.ContinueOnError)
	flags.SetOutput(stderr)
	mode := flags.String("mode", "report", "report | generate | verify | docs | product-claims | budget-proof | remote-validation | graph-read-probe")
	specsDir := flags.String("specs", defaultSpecsDir, "path to the specs directory")
	out := flags.String("out", defaultArtifactOut, "catalog artifact output path (generate mode)")
	surfaceOut := flags.String("surface-out", defaultSurfaceArtifactOut, "surface inventory artifact output path (generate mode)")
	budgetArtifact := flags.String("budget-artifact", "", "public capability budget proof artifact path (budget-proof mode)")
	docsDir := flags.String("docs", defaultDocsDir, "path to the docs directory (docs and product-claims modes)")
	root := flags.String("root", defaultRoot, "path to the repository root (surface enumeration, remote-validation mode)")
	remoteValidationBaseline := flags.String("remote-validation-baseline", defaultRemoteValidationBaseline, "path to the remote_validation burn-down baseline (remote-validation mode)")
	remoteValidationUpdate := flags.Bool("update", false, "regenerate the remote-validation baseline from the current tree instead of checking it (remote-validation mode)")
	apiBaseURL := flags.String("api-base-url", os.Getenv("ESHU_API_BASE_URL"), "API base URL (graph-read-probe mode)")
	mcpURL := flags.String("mcp-url", os.Getenv("ESHU_MCP_URL"), "exact MCP HTTP endpoint URL (graph-read-probe mode)")
	userTokenEnv := flags.String("user-token-env", "ESHU_MCP_TOKEN", "environment variable containing a user bearer token (graph-read-probe mode)")
	adminTokenEnv := flags.String("admin-token-env", "ESHU_API_KEY", "environment variable containing an admin/all-scope bearer token (graph-read-probe mode)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *mode == "budget-proof" {
		return checkBudgetProof(stdout, *specsDir, *budgetArtifact)
	}

	if *mode == "remote-validation" {
		return checkRemoteValidation(stdout, *specsDir, *root, *remoteValidationBaseline, *remoteValidationUpdate)
	}

	if *mode == "graph-read-probe" {
		return checkGraphReadProbeMode(stdout, *apiBaseURL, *mcpURL, *userTokenEnv, *adminTokenEnv)
	}

	signals := mcpSignals()
	catalog, findings, err := capabilitycatalog.BuildFromSpecs(*specsDir, signals)
	if err != nil {
		return err
	}

	if *mode == "docs" {
		ledgerPath := filepath.Join(*specsDir, capabilitycatalog.ProductClaimLedgerFileName)
		return checkDocs(stdout, catalog, *docsDir, ledgerPath, *root)
	}

	if *mode == "product-claims" {
		ledgerPath := filepath.Join(*specsDir, capabilitycatalog.ProductClaimLedgerFileName)
		return checkProductClaimsMode(stdout, catalog, *docsDir, ledgerPath, *root)
	}

	surfaceInventory, surfaceFindings, err := buildSurfaceInventory(*specsDir, *root)
	if err != nil {
		return err
	}

	switch *mode {
	case "report":
		writeFindings(stdout, findings)
		writeSurfaceFindings(stdout, surfaceFindings)
		_, err = fmt.Fprintf(stdout, "catalog entries: %d\nsurface records: %d\n",
			len(catalog.Entries), len(surfaceInventory.Surfaces))
		return err
	case "generate":
		payload, err := capabilitycatalog.MarshalCatalog(catalog)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*out, payload, 0o600); err != nil {
			return fmt.Errorf("write artifact %s: %w", *out, err)
		}
		surfacePayload, err := capabilitycatalog.MarshalSurfaceInventory(surfaceInventory)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*surfaceOut, surfacePayload, 0o600); err != nil {
			return fmt.Errorf("write surface artifact %s: %w", *surfaceOut, err)
		}
		writeFindings(stdout, findings)
		writeSurfaceFindings(stdout, surfaceFindings)
		_, err = fmt.Fprintf(stdout, "wrote %s (%d entries)\nwrote %s (%d surfaces)\n",
			*out, len(catalog.Entries), *surfaceOut, len(surfaceInventory.Surfaces))
		return err
	case "verify":
		payload, err := capabilitycatalog.MarshalCatalog(catalog)
		if err != nil {
			return err
		}
		surfacePayload, err := capabilitycatalog.MarshalSurfaceInventory(surfaceInventory)
		if err != nil {
			return err
		}
		return verify(stdout, payload, findings, surfacePayload, surfaceFindings)
	default:
		return fmt.Errorf("unsupported mode %q", *mode)
	}
}

// checkBudgetProof verifies an operator-supplied public-safe measurement
// artifact against the capability matrix's per-profile performance budgets.
func checkBudgetProof(stdout io.Writer, specsDir, artifactPath string) error {
	if artifactPath == "" {
		return fmt.Errorf("-budget-artifact is required in budget-proof mode")
	}
	matrix, err := capabilitycatalog.LoadMatrix(specsDir)
	if err != nil {
		return err
	}
	artifact, err := capabilitycatalog.LoadBudgetProofArtifact(artifactPath)
	if err != nil {
		return err
	}
	findings := capabilitycatalog.CheckBudgetProof(matrix, artifact)
	if len(findings) == 0 {
		_, err := fmt.Fprintf(stdout, "capability budget proof verified: measurements=%d\n", len(artifact.Measurements))
		return err
	}
	_, _ = fmt.Fprintf(stdout, "%d capability budget proof findings:\n", len(findings))
	for _, finding := range findings {
		_, _ = fmt.Fprintf(stdout, "  [%s] %s: %s\n", finding.Kind, finding.Subject, finding.Detail)
	}
	return fmt.Errorf("capability budget proof failed: %d findings", len(findings))
}

// checkDocs runs the docs guards: capability-state freshness, collector-state
// readiness, and the product claim-to-proof ledger.
func checkDocs(stdout io.Writer, catalog capabilitycatalog.Catalog, docsDir, ledgerPath, root string) error {
	claims, err := capabilitycatalog.ParseDocClaims(docsDir)
	if err != nil {
		return err
	}
	docFindings := capabilitycatalog.CheckDocFreshness(catalog, claims)
	if len(docFindings) == 0 {
		_, _ = fmt.Fprintf(stdout, "checked %d capability-state claims; no freshness findings\n", len(claims))
	} else {
		_, _ = fmt.Fprintf(stdout, "%d docs freshness findings:\n", len(docFindings))
		for _, finding := range docFindings {
			_, _ = fmt.Fprintf(stdout, "  %s:%d [%s] claimed=%s expected=%s: %s\n",
				finding.Path, finding.Line, finding.Capability, finding.Claimed, finding.Expected, finding.Reason)
		}
	}

	collectorFindings, err := checkCollectorDocs(stdout, docsDir)
	if err != nil {
		return err
	}

	productFindings, err := checkProductClaims(stdout, catalog, ledgerPath, docsDir, root)
	if err != nil {
		return err
	}

	total := len(docFindings) + len(collectorFindings) + len(productFindings)
	if total > 0 {
		return fmt.Errorf("docs freshness check failed: %d capability findings, %d collector findings, %d product claim findings",
			len(docFindings), len(collectorFindings), len(productFindings))
	}
	return nil
}

// checkProductClaimsMode runs only the product claim ledger guard: every
// guarded product-claim marker must resolve to exactly one ledger row with a
// valid proof chain, and (when ESHU_VERIFY_PRODUCT_CLAIM_ISSUES_LIVE=1) every
// issue-backed claim's recorded state must match GitHub.
//
// Unlike -mode docs (checkDocs), this mode does not scan capability-state or
// collector-state markers. mcp-schema-drift.yml already runs the full
// -mode docs docs-tree scan on every PR, so the product-claim-ledger workflow
// uses this narrower mode instead of repeating that scan just to reach the
// product-claim ledger check and the live issue-state guard it adds. See
// #4073.
func checkProductClaimsMode(stdout io.Writer, catalog capabilitycatalog.Catalog, docsDir, ledgerPath, root string) error {
	findings, err := checkProductClaims(stdout, catalog, ledgerPath, docsDir, root)
	if err != nil {
		return err
	}
	if len(findings) > 0 {
		return fmt.Errorf("product claim ledger check failed: %d findings", len(findings))
	}
	return nil
}

func checkProductClaims(stdout io.Writer, catalog capabilitycatalog.Catalog, ledgerPath, docsDir, root string) ([]capabilitycatalog.ProductClaimFinding, error) {
	ledger, err := capabilitycatalog.LoadProductClaimLedger(ledgerPath)
	if err != nil {
		return nil, err
	}
	inventory, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		return nil, err
	}
	findings := capabilitycatalog.CheckProductClaims(root, catalog, inventory, ledger)
	markers, err := capabilitycatalog.ParseProductClaimMarkers(root, docsDir)
	if err != nil {
		return nil, err
	}
	findings = append(findings, capabilitycatalog.CheckProductClaimMarkers(ledger, markers)...)
	if os.Getenv(envVerifyIssues) == "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		issueFindings := capabilitycatalog.CheckProductClaimIssueStates(
			ctx,
			nil,
			"",
			"eshu-hq/eshu",
			os.Getenv(envGitHubToken),
			ledger,
		)
		findings = append(findings, issueFindings...)
	}
	if len(findings) == 0 {
		_, _ = fmt.Fprintf(stdout, "checked %d product claims; no ledger findings\n", len(ledger.Claims))
		return nil, nil
	}
	_, _ = fmt.Fprintf(stdout, "%d product claim ledger findings:\n", len(findings))
	for _, finding := range findings {
		_, _ = fmt.Fprintf(stdout, "  %s:%d [%s] %s: %s\n",
			finding.Path, finding.Line, finding.ID, finding.Kind, finding.Detail)
	}
	return findings, nil
}

// checkCollectorDocs runs the collector readiness guard against the embedded
// surface inventory and prints any findings. It returns the findings so the
// caller can fail the command, and an error only for an unreadable inventory or
// docs tree.
func checkCollectorDocs(stdout io.Writer, docsDir string) ([]capabilitycatalog.CollectorFinding, error) {
	inv, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		return nil, err
	}
	collectorClaims, err := capabilitycatalog.ParseCollectorClaims(docsDir)
	if err != nil {
		return nil, err
	}
	findings := capabilitycatalog.CheckCollectorReadiness(inv, collectorClaims)
	if len(findings) == 0 {
		_, _ = fmt.Fprintf(stdout, "checked %d collector-state claims; no readiness findings\n", len(collectorClaims))
		return nil, nil
	}
	_, _ = fmt.Fprintf(stdout, "%d collector readiness findings:\n", len(findings))
	for _, finding := range findings {
		_, _ = fmt.Fprintf(stdout, "  %s:%d [%s] claimed=%s expected=%s: %s\n",
			finding.Path, finding.Line, finding.Collector, finding.Claimed, finding.Expected, finding.Reason)
	}
	return findings, nil
}

// verify fails when reconciliation findings exist or when either embedded
// artifact differs from its freshly generated payload. The comparison uses the
// raw embedded bytes so any deviation in a committed artifact is caught,
// including a surface added or removed in code without regenerating.
func verify(
	stdout io.Writer,
	payload []byte,
	findings []capabilitycatalog.Finding,
	surfacePayload []byte,
	surfaceFindings []capabilitycatalog.Finding,
) error {
	writeFindings(stdout, findings)
	writeSurfaceFindings(stdout, surfaceFindings)

	catalogStale := !bytes.Equal(payload, capabilitycatalog.RawArtifact())
	if catalogStale {
		_, _ = fmt.Fprintln(stdout, "embedded catalog artifact is stale; run: go run ./cmd/capability-inventory -mode generate")
	}
	surfaceStale := !bytes.Equal(surfacePayload, capabilitycatalog.RawSurfaceArtifact())
	if surfaceStale {
		_, _ = fmt.Fprintln(stdout, "embedded surface inventory artifact is stale; run: go run ./cmd/capability-inventory -mode generate")
	}

	failed := len(findings) > 0 || len(surfaceFindings) > 0 || catalogStale || surfaceStale
	if failed {
		return fmt.Errorf(
			"capability inventory verification failed: %d catalog findings, %d surface findings, catalog_stale=%v, surface_stale=%v",
			len(findings), len(surfaceFindings), catalogStale, surfaceStale,
		)
	}
	_, err := fmt.Fprintln(stdout, "capability catalog and surface inventory verified")
	return err
}

// writeSurfaceFindings prints surface reconciliation findings for report and
// verify modes.
func writeSurfaceFindings(stdout io.Writer, findings []capabilitycatalog.Finding) {
	if len(findings) == 0 {
		_, _ = fmt.Fprintln(stdout, "no surface reconciliation findings")
		return
	}
	_, _ = fmt.Fprintf(stdout, "%d surface reconciliation findings:\n", len(findings))
	for _, finding := range findings {
		_, _ = fmt.Fprintf(stdout, "  [%s] %s: %s\n", finding.Kind, finding.Subject, finding.Detail)
	}
}

func writeFindings(stdout io.Writer, findings []capabilitycatalog.Finding) {
	if len(findings) == 0 {
		_, _ = fmt.Fprintln(stdout, "no reconciliation findings")
		return
	}
	_, _ = fmt.Fprintf(stdout, "%d reconciliation findings:\n", len(findings))
	for _, finding := range findings {
		_, _ = fmt.Fprintf(stdout, "  [%s] %s: %s\n", finding.Kind, finding.Subject, finding.Detail)
	}
}

// mcpSignals collects the live MCP tool registry into reconciliation signals.
func mcpSignals() capabilitycatalog.Signals {
	tools := mcp.ReadOnlyTools()
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	return capabilitycatalog.Signals{MCPTools: names}
}
