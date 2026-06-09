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
// narrative, bounded DOCX summaries, conservative delimited spreadsheet files,
// bounded XLSX workbook summaries, bounded PPTX slide summaries, bounded ZIP
// documentation packets, and deterministic Mermaid/D2 text diagrams plus
// structured PlantUML, Draw.io, Excalidraw, and SVG diagrams become
// source-neutral documentation facts. DOCX
// comments and tracked changes stay metadata-only; legacy XLS files are
// classified as unsupported binary workbooks without reading cell bytes; PPTX
// hidden slides, speaker notes, and comments stay metadata-only while visible
// content still emits facts. External relationships, embedded objects, macro
// content, malformed containers, unsafe paths, resource limits, and compression
// hazards block Office extraction. ZIP/TAR archives preserve normalized member paths
// and contained content hashes for allowed documents, while unsafe paths,
// symlinks, special files, nested archives, credential-like members, unsupported
// formats, and compression hazards stay warning-only. Prose surfaces may emit
// non-authoritative document-evidence claim candidates, but API contract
// operations, schemas, channels, GraphQL SDL fields, spreadsheet cells, slide
// text, archive membership, and diagram labels or links remain documentation
// evidence; they do not prove service ownership.
// Default-off media transcript helpers can build timestamped documentation
// facts from reviewed local transcript output after media preflight, but media
// files are not enabled in repository discovery by this package.
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
