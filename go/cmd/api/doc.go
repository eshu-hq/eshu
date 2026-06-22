// Package main runs the eshu-api binary, which serves the Eshu HTTP query and
// admin surface backed by the configured graph backend and Postgres content
// store. `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER=hash` or `local_hash` explicitly
// forces deterministic no-network local semantic/hybrid retrieval from ready
// persisted vector rows over active curated search documents. `auto_hash`
// selects one governed search_documents provider profile when configured and
// otherwise falls back to local hash query embeddings.
// `ESHU_SAML_PROVIDERS_JSON` optionally enables SAML service-provider metadata,
// login, and ACS routes using IdP metadata loaded from a private environment
// handle and hash-only request/replay/session storage.
//
// When invoked with --version or -v, it prints the embedded application
// version through the test-covered printAPIVersionFlag helper and exits before
// runtime setup. Otherwise the binary boots OTEL telemetry, wires the query
// router and the shared runtime admin mux, including Postgres-backed supply
// chain attachment, advisory evidence, work-item evidence, impact finding,
// impact explanation reads, admission decision readback, optional scoped-token
// registry authentication, hash-only dashboard browser-session cookies,
// optional backend OIDC login that maps hashed external groups to Eshu role
// grants before issuing bounded-staleness browser-session cookies,
// optional redacted semantic provider profile status, optional semantic
// extraction source policy, optional hosted governance status readback from
// safe ESHU_GOVERNANCE_* metadata, optional component-extension registry
// diagnostics when ESHU_COMPONENT_HOME is set, and an optional
// Prometheus/Mimir metrics time-series source for console trends. It listens on
// ESHU_API_ADDR (default :8080) wrapped in otelhttp instrumentation. On SIGINT
// or SIGTERM it gives the HTTP server up to five seconds for graceful shutdown
// before exiting.
// Beyond auth/session ledgers, the runtime serves reads only; it does not own
// repo sync, parsing, fact emission, vector builds, or queued projection work.
//
// When ESHU_PPROF_ADDR is set, the binary also exposes an opt-in
// net/http/pprof endpoint via runtime.NewPprofServer, bound to the loopback
// interface for port-only inputs so the default does not reach beyond the local
// host.
package main
