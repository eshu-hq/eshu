# TypeScript Dead-Code Corpus Evidence

This page records the issue #336 JavaScript-family corpus check. It is evidence
for hardening TypeScript and JavaScript reachability roots. It does not promote
TypeScript or TSX candidates to cleanup-safe dead-code truth.

## Corpus Pass

The pass used shallow filtered clones of these pinned open-source heads:

- `fastify/fastify@1c49974805e57a17f1616f422678317b1d35b73f`: 292
  JavaScript-family files and 388 `route({ handler })` or handler-object
  matches.
- `taskforcesh/bullmq@ec2efcf4493479f9e7ecd0ae8a9000a831bb0084`: 171
  JavaScript-family files and 677 `new Worker(...)` matches.
- `nestjs/nest@983dd52c4927753be3421162fc43e4fde8d3fcde`: 1,675
  JavaScript-family files with 618 controller or constructor-injection matches.

The Fastify sample exposed a route-object handler gap. The BullMQ sample
exposed a constructor function-value gap. NestJS controller routes and
TypeScript constructor/property receiver calls were already covered by parser
and reducer fixtures.

Eshu now models Fastify route-object handler roots and function-value
references passed through route object `handler` properties and constructor
arguments such as BullMQ processors. Full command/API candidate sampling and
manual precision labeling remain required before TypeScript can claim exact
cleanup safety.

## Fastify Declaration API Sampling

Follow-up sampling used the same pinned Fastify checkout through a fresh
local-authoritative run on NornicDB `v1.0.44`.

The baseline from `main@d13c863` returned five active TypeScript candidates
from the bounded investigation API:

```json
{
  "repo_id": "fastify",
  "language": "typescript",
  "limit": 25,
  "offset": 0
}
```

Manual labels:

| Candidate | File | Baseline bucket | Label | Root cause |
| --- | --- | --- | --- | --- |
| `FastifyRequestContext` | `types/context.d.ts` | ambiguous | public API, not dead | `fastify.d.ts` imports then exports the type in a local `export type { ... }` block. |
| `FastifyReplyContext` | `types/context.d.ts` | ambiguous | public API, not dead | Same local imported export clause; inline trailing comments hid one neighbor name. |
| `FastifyRequest` | `types/request.d.ts` | ambiguous | public API, not dead | Same local imported export clause. |
| `FastifyValidationResult` | `types/schema.d.ts` | ambiguous | public API dependency, not dead | Referenced from public schema compiler declarations. |
| `ResolveFastifyRequestType` | `types/type-provider.d.ts` | ambiguous | public API dependency, not dead | Used by `FastifyRequest` as an imported generic default in the public declaration surface. |

The parser now models:

- package declaration entrypoints that import symbols and export them through
  a local `export type { ... }` clause without a `from` source;
- inline `//` comments inside multi-line export specifier lists;
- imported type references used by public TypeScript declarations, bounded by
  the package public surface and repository-local import resolution.

Final API evidence from the same request after the parser fix:

```json
{
  "bucket_counts": {
    "ambiguous": 0,
    "cleanup_ready": 0,
    "suppressed": 50,
    "suppressed_truncated": true
  },
  "coverage": {
    "candidate_scan_rows": 403,
    "candidate_scan_pages": 5,
    "candidate_scan_limit": 2500,
    "candidate_scan_truncated": false,
    "file_count": 324,
    "entity_count": 8274,
    "language": "typescript",
    "truncated": false
  }
}
```

Accuracy result: the Fastify sample removed 5/5 false-positive ambiguous
TypeScript candidates and emitted zero cleanup-ready TypeScript candidates.
TypeScript remains `derived`, not exact cleanup-safe truth.

Performance Evidence: the final Fastify local-authoritative proof indexed 324
files and 8,274 content entities. Collector stream wall time was 2.9154 s,
content write was 0.5262 s, code-call projection was 0.4243 s, and terminal
queue state was pending=0, in_flight=0, retrying=0, dead_letter=0, failed=0.

No-Regression Evidence: focused TDD failures for Fastify route-object handlers
and constructor function-value references were added first. The parser and
reducer regression commands were:

```bash
cd go
go test ./internal/parser -run 'TestDefaultEngineParsePathTypeScriptFastifyRouteObjectHandler' -count=1
go test ./internal/reducer -run 'TestExtractCodeCallRowsResolvesFastifyRouteObjectHandlerReference|TestExtractCodeCallRowsResolvesConstructorFunctionValueReference' -count=1
```

Benchmark Evidence: on Apple M1 Max / darwin arm64, the reducer dynamic-call
benchmark stayed within run noise. The branch `-count=3` run measured 8.616,
8.687, and 8.640 ms/op with 29,699 to 29,700 allocs/op.

Observability Evidence: no new read-path query or worker stage was added.
Existing code-call materialization logs report fact, repo, row, intent, extract,
build, upsert, and total duration fields. Existing bounded dead-code
investigation coverage reports candidate scan pages, rows, limit, and
truncation fields.
