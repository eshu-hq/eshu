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
	out := flags.String("out", defaultArtifactOut, "artifact output path (generate mode)")
	docsDir := flags.String("docs", defaultDocsDir, "path to the docs directory (docs mode)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	signals := mcpSignals()
	catalog, findings, err := capabilitycatalog.BuildFromSpecs(*specsDir, signals)
	if err != nil {
		return err
	}

	switch *mode {
	case "report":
		writeFindings(stdout, findings)
		_, err = fmt.Fprintf(stdout, "catalog entries: %d\n", len(catalog.Entries))
		return err
	case "generate":
		payload, err := capabilitycatalog.MarshalCatalog(catalog)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*out, payload, 0o644); err != nil {
			return fmt.Errorf("write artifact %s: %w", *out, err)
		}
		writeFindings(stdout, findings)
		_, err = fmt.Fprintf(stdout, "wrote %s (%d entries)\n", *out, len(catalog.Entries))
		return err
	case "verify":
		payload, err := capabilitycatalog.MarshalCatalog(catalog)
		if err != nil {
			return err
		}
		return verify(stdout, payload, findings)
	case "docs":
		return checkDocs(stdout, catalog, *docsDir)
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

// verify fails when reconciliation findings exist or when the embedded artifact
// differs from the freshly generated payload. The comparison uses the raw
// embedded bytes so any deviation in the committed artifact is caught.
func verify(stdout io.Writer, payload []byte, findings []capabilitycatalog.Finding) error {
	writeFindings(stdout, findings)
	stale := !bytes.Equal(payload, capabilitycatalog.RawArtifact())
	if stale {
		_, _ = fmt.Fprintln(stdout, "embedded catalog artifact is stale; run: go run ./cmd/capability-inventory -mode generate")
	}
	if len(findings) > 0 || stale {
		return fmt.Errorf("capability catalog verification failed: %d findings, stale=%v", len(findings), stale)
	}
	_, err := fmt.Fprintln(stdout, "capability catalog verified")
	return err
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
