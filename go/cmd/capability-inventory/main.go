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
	mode := flags.String("mode", "report", "report | generate | verify")
	specsDir := flags.String("specs", defaultSpecsDir, "path to the specs directory")
	out := flags.String("out", defaultArtifactOut, "artifact output path (generate mode)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	signals := mcpSignals()
	catalog, findings, err := capabilitycatalog.BuildFromSpecs(*specsDir, signals)
	if err != nil {
		return err
	}
	payload, err := capabilitycatalog.MarshalCatalog(catalog)
	if err != nil {
		return err
	}

	switch *mode {
	case "report":
		writeFindings(stdout, findings)
		_, err = fmt.Fprintf(stdout, "catalog entries: %d\n", len(catalog.Entries))
		return err
	case "generate":
		if err := os.WriteFile(*out, payload, 0o644); err != nil {
			return fmt.Errorf("write artifact %s: %w", *out, err)
		}
		writeFindings(stdout, findings)
		_, err = fmt.Fprintf(stdout, "wrote %s (%d entries)\n", *out, len(catalog.Entries))
		return err
	case "verify":
		return verify(stdout, payload, findings)
	default:
		return fmt.Errorf("unsupported mode %q", *mode)
	}
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
