// Package buildinfo exposes the application version injected at build time.
//
// Version is set via -ldflags during release builds and defaults to "dev" for
// source builds. AppVersion is the normalized accessor used by runtime, API,
// and MCP surfaces; explicit linker values win, then Go module build-info
// versions from go install ...@version, then "dev" for local source builds.
// PrintVersionFlag gives service binaries a shared early-exit path for
// --version and -v probes before they open datastores or telemetry providers.
package buildinfo
