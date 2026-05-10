// Package discovery resolves parser-supported files into stable repo-root file
// sets for the Go collector.
//
// Discovery applies repo-local .gitignore and .eshuignore files, including
// root-anchored gitignore patterns, operator overlays, hidden/generated/vendor
// skip rules, nested Git repository preference, and deterministic sorting
// before the collector snapshots files.
package discovery
