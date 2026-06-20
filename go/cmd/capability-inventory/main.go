package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/mcp"
)

const (
	defaultSpecsDir    = "../specs"
	defaultDocsDir     = "../docs/public"
	defaultRoot        = ".."
	defaultArtifactOut = "internal/capabilitycatalog/data/catalog.generated.json"
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
	mode := flags.String("mode", "report", "report | generate | verify | docs")
	specsDir := flags.String("specs", defaultSpecsDir, "path to the specs directory")
	out := flags.String("out", defaultArtifactOut, "catalog artifact output path (generate mode)")
	surfaceOut := flags.String("surface-out", defaultSurfaceArtifactOut, "surface inventory artifact output path (generate mode)")
	docsDir := flags.String("docs", defaultDocsDir, "path to the docs directory (docs mode)")
	root := flags.String("root", defaultRoot, "path to the repository root (surface enumeration)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	signals := mcpSignals()
	catalog, findings, err := capabilitycatalog.BuildFromSpecs(*specsDir, signals)
	if err != nil {
		return err
	}

	// Docs freshness checks only the capability catalog and never enumerates the
	// source tree, so it does not need (and must not require) a repo root.
	if *mode == "docs" {
		return checkDocs(stdout, catalog, *docsDir)
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
		if err := os.WriteFile(*out, payload, 0o644); err != nil {
			return fmt.Errorf("write artifact %s: %w", *out, err)
		}
		surfacePayload, err := capabilitycatalog.MarshalSurfaceInventory(surfaceInventory)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*surfaceOut, surfacePayload, 0o644); err != nil {
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

// checkDocs scans the docs directory for capability-state markers and fails when
// any marker contradicts the catalog. It is the docs freshness guard.
func checkDocs(stdout io.Writer, catalog capabilitycatalog.Catalog, docsDir string) error {
	claims, err := capabilitycatalog.ParseDocClaims(docsDir)
	if err != nil {
		return err
	}
	docFindings := capabilitycatalog.CheckDocFreshness(catalog, claims)
	if len(docFindings) == 0 {
		_, err = fmt.Fprintf(stdout, "checked %d capability-state claims; no freshness findings\n", len(claims))
		return err
	}
	_, _ = fmt.Fprintf(stdout, "%d docs freshness findings:\n", len(docFindings))
	for _, finding := range docFindings {
		_, _ = fmt.Fprintf(stdout, "  %s:%d [%s] claimed=%s expected=%s: %s\n",
			finding.Path, finding.Line, finding.Capability, finding.Claimed, finding.Expected, finding.Reason)
	}
	return fmt.Errorf("docs freshness check failed: %d findings", len(docFindings))
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
			len(findings), len(surfaceFindings), catalogStale, surfaceStale)
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
