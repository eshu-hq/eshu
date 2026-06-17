package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/eshu-hq/eshu/go/internal/auditreport"
	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run generates a deterministic competitive-audit report from a declarative
// audit input, reconciled against the embedded capability catalog and an
// optional open-issues list for duplicate detection. It never creates issues.
func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("audit-report", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputPath := flags.String("input", "", "path to the audit input YAML (required)")
	issuesPath := flags.String("issues", "", "path to open issues JSON from `gh issue list --json number,title` (optional)")
	format := flags.String("format", "md", "report format: md or json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *inputPath == "" {
		return fmt.Errorf("-input is required")
	}

	input, err := auditreport.LoadInput(*inputPath)
	if err != nil {
		return err
	}
	catalog, err := capabilitycatalog.Load()
	if err != nil {
		return err
	}
	issues, err := loadIssues(*issuesPath)
	if err != nil {
		return err
	}

	report := auditreport.Generate(input, catalog, issues)
	switch *format {
	case "md", "markdown":
		_, err = io.WriteString(stdout, auditreport.RenderMarkdown(report))
		return err
	case "json":
		payload, err := auditreport.RenderJSON(report)
		if err != nil {
			return err
		}
		_, err = stdout.Write(payload)
		return err
	default:
		return fmt.Errorf("unsupported format %q", *format)
	}
}

func loadIssues(path string) ([]auditreport.OpenIssue, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read issues %s: %w", path, err)
	}
	var issues []auditreport.OpenIssue
	if err := json.Unmarshal(raw, &issues); err != nil {
		return nil, fmt.Errorf("parse issues %s: %w", path, err)
	}
	return issues, nil
}
