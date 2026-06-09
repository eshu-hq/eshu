# AGENTS.md - internal/semanticdocs guidance for LLM assistants

## Read first

1. `go/internal/semanticdocs/README.md`
2. `go/internal/semanticdocs/doc.go`
3. `go/internal/semanticdocs/emitter.go`
4. `go/internal/doctruth/README.md`
5. `go/internal/facts/semantic.go`
6. `docs/internal/design/1758-documentation-semantic-observations.md`

## Invariants

- Keep this package pure. No provider clients, HTTP calls, database access,
  graph writes, queue consumers, goroutines, or telemetry side effects belong
  here.
- Emit only `semantic.documentation_observation` facts and validate every
  payload with `facts.ValidateSemanticDocumentationObservationPayload`.
- Treat model output as evidence. Do not add canonical admission states, graph
  promotion, or query truth here.
- Preserve section provenance from `doctruth.SectionInput`: scope, generation,
  source system, document id, revision/source hash, section id, URI, excerpt
  hash, and observed time.
- Store provider profile handles and version strings, not prompt bodies,
  request bodies, raw provider responses, credentials, bearer tokens, or secret
  values.
- Unsafe redaction state must fail closed by forcing provenance-only output and
  dropping observation text.

## Common changes and how to scope them

- Add a semantic observation field only when the existing `facts` payload
  supports it. If the durable fact contract changes, update `go/internal/facts`
  and public fact-envelope docs in the same PR.
- Add provider execution in a different package. This package should keep
  accepting already-redacted, parsed output.
- Add API or MCP readback in the owning query or MCP package after schema and
  security review; do not route reads from this package.
- Add reducer admission in `go/internal/reducer`, with deterministic
  corroboration tests before any finding promotion.

## Failure modes

- Missing provider profile or provider kind means the emitter is misconfigured;
  return an error instead of falling back to raw credentials.
- Missing section revision or excerpt hash means replay provenance is
  incomplete; do not emit an observation.
- Unsupported admission state means model output is trying to bypass reducer
  admission; fail closed.
- Unsafe redaction means no observation text survives in the payload.

## Anti-patterns

- Calling an LLM provider or storing provider request/response bodies.
- Using observation text as deployment, ownership, runtime, vulnerability, or
  infrastructure truth.
- Adding raw source content, private URLs, customer names, credentials, API
  keys, tokens, local hostnames, or filesystem paths to tests or docs.
- Growing `doctruth` with semantic provider behavior.
