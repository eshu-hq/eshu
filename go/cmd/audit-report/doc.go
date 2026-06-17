// Command audit-report generates a deterministic competitive-audit report.
//
// It reads an operator-authored audit input (YAML), reconciles it against the
// embedded capability catalog and an optional open-issues JSON for duplicate
// detection, and prints a Markdown or JSON report. It never creates issues.
//
//	go run ./cmd/audit-report -input audit.yaml -format md
//	gh issue list --json number,title > issues.json
//	go run ./cmd/audit-report -input audit.yaml -issues issues.json -format json
package main
