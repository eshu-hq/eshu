// Package main runs the eshu-api binary, which serves the Eshu HTTP query and
// admin surface backed by the configured graph backend and Postgres content
// store.
//
// When invoked with --version or -v, it prints the embedded application
// version through the test-covered printAPIVersionFlag helper and exits before
// runtime setup. Otherwise the binary boots OTEL telemetry, wires the query router and the shared
// runtime admin mux, and listens on ESHU_API_ADDR (default :8080) wrapped in
// otelhttp instrumentation. On SIGINT or SIGTERM it gives the HTTP server up
// to five seconds for graceful shutdown before exiting. The runtime serves
// reads only; it does not own repo sync, parsing, fact emission, or queued
// projection work.
//
// When ESHU_PPROF_ADDR is set, the binary also exposes an opt-in
// net/http/pprof endpoint via runtime.NewPprofServer, bound to 127.0.0.1
// for port-only inputs so the default does not reach beyond the local host.
package main
