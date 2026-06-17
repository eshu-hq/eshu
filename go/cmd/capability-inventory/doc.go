// Command capability-inventory generates and verifies the reconciled Eshu
// capability catalog artifact.
//
// It loads the capability matrix and the editorial overlay from the specs
// directory, reconciles them against the live MCP tool registry, and writes the
// deterministic catalog artifact embedded by
// github.com/eshu-hq/eshu/go/internal/capabilitycatalog.
//
// Modes:
//
//	report    print reconciliation findings and the entry count (default)
//	generate  write the catalog artifact to -out
//	verify    fail when findings exist or the embedded artifact is stale
//	docs      fail when a docs capability-state marker contradicts the catalog
//
// Run from the go module directory:
//
//	go run ./cmd/capability-inventory -mode generate
//	go run ./cmd/capability-inventory -mode verify
package main
