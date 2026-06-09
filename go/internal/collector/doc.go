// Package collector owns source observation, repository selection, snapshot
// capture, parser input shaping, and fact streaming for Eshu indexing runs.
//
// The package turns selected sources into cloned or native filesystem
// snapshots, discovery reports, parser metadata, content entity snapshots, and
// facts.Envelope streams. It is the source of truth for snapshot input shape,
// but graph projection and query-time truth belong to downstream projector,
// reducer, storage, and query packages.
//
// Collection is best-effort over remote and local filesystems. Callers must
// handle partial snapshots, discovery skips, webhook-triggered refreshes, claim
// fencing, and batch-drain hooks explicitly. Raw Terraform-state bytes do not
// enter normal repository snapshots; only metadata-only state candidates are
// emitted for the Terraform-state collector path to approve and read.
// Repository-hosted Markdown, lightweight text, HTML, API contracts, notebook
// narrative, conservative delimited spreadsheet files, and deterministic
// Mermaid/D2 text diagrams become source-neutral documentation facts. Prose
// surfaces may emit non-authoritative document-evidence claim candidates, but
// API contract operations, schemas, channels, GraphQL SDL fields, and
// text-diagram labels or links remain documentation evidence; they do not prove
// service ownership.
// Notebook code-cell source remains parser evidence; Markdown cells, raw cells,
// and selected stdout/text outputs are the only notebook content that enters
// the documentation lane. Declared Grafana, Prometheus/Mimir, Loki, and Tempo
// observability rows plus applied
// Argo CD/Kubernetes observability state rows from repository parsers become
// metadata-only observability source facts; reducers and query surfaces own any
// later declared/applied/observed coverage truth.
//
// The scannerworker subpackage owns the hosted boundary for isolated security
// analyzers. It defines claim input, target scope, resource limits,
// source-fact output validation, retry/dead-letter payloads, and the claim loop
// used by scanner-worker runtimes while reducers keep finding truth ownership.
package collector
