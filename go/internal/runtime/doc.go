// Package runtime provides shared process runtime contracts for Eshu services.
//
// The package owns admin HTTP surfaces, metrics endpoints, the opt-in
// net/http/pprof endpoint, lifecycle wiring, retry policy defaults, API key
// checks, auto-generated local API key state, and data-store configuration
// shared by the API, MCP, ingester, reducer, and helper binaries.
package runtime
