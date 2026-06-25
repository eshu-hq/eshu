module github.com/eshu-hq/eshu/tools/golangci-lint-filelength

go 1.26.0

// Pinned to the exact revision golangci-lint v2.12.2 vendors
// (see go/pkg/mod/github.com/golangci/golangci-lint/v2@v2.12.2/go.mod).
// A Go plugin loaded via plugin.Open must be built against the same
// `golang.org/x/tools` revision the host binary uses, otherwise the
// load fails with "plugin was built with a different version of
// package ...". Keep these two versions in lockstep when bumping
// golangci-lint.
require golang.org/x/tools v0.44.0
