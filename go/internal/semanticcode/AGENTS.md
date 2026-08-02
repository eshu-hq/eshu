# AGENTS.md - internal/semanticcode guidance for LLM assistants

## Read first

1. `go/internal/semanticcode/README.md`
2. `go/internal/semanticcode/doc.go`
3. `go/internal/semanticcode/emitter.go`
4. `go/internal/semanticdocs/AGENTS.md` — the documentation twin; keep the two
   packages' invariants aligned rather than letting them drift apart
5. `go/internal/facts/semantic.go`

## Invariants

- Keep this package pure. No provider clients, HTTP calls, database access,
  graph writes, queue consumers, goroutines, or telemetry side effects belong
  here.
- Emit only `semantic.code_hint` facts, and validate every payload with
  `facts.ValidateSemanticCodeHintPayload` before returning it.
- Treat model output as evidence, never as truth. `PromotionPolicy` is not
  configurable and must stay
  `facts.SemanticPromotionRequiresDeterministicEvidence`; a caller that could
  relax it would make the read surface's non-canonical label a lie.
- Default `CorroborationState` to `uncorroborated`. "Nothing has checked this
  yet" is a real answer and the field exists to give it.
- Preserve span provenance from `CodeSpanInput`: scope, generation, source
  system, repository, path, span id, canonical URI, content hash, and lines.
  A hint a reader cannot follow back to code is not shippable evidence.
- Store provider profile handles and version strings only. Never prompt bodies,
  request bodies, raw provider responses, credentials, or tokens.
- Fail closed. `Emit` returns an error rather than a partial batch: silently
  dropping an inadmissible hint reproduces the empty-answer defect (#5693) this
  package exists to fix.

## Changing the payload

`SemanticCodeHintPayload` is a contract with a checked-in JSON schema
(`sdk/go/factschema/schema/semantic.code_hint.v1.schema.json`) and a registry
entry. Route any shape change through `eshu-contract-rigor` — a payload change
here moves the fact-kind registry, the schema artifact, and the read model
together, not one at a time.

## Proving a change

- `cd go && go test ./internal/semanticcode ./internal/facts ./internal/query -count=1`
- A change to what the emitter produces also moves the golden corpus: the
  code-hint read shape is asserted by the B-7 gate. Follow
  `eshu-golden-corpus-rigor` and update the cassette and the B-12 snapshot in
  the same change.
