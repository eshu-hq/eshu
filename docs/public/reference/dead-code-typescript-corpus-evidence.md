# TypeScript Dead-Code Corpus Evidence

This note records the issue #336 JavaScript-family corpus check. It is evidence
for hardening TypeScript and JavaScript reachability roots, not a promotion to
exact cleanup-safe dead-code truth.

## Corpus Pass

The pass used shallow filtered clones of current open-source heads:

- `fastify/fastify@1c49974805e57a17f1616f422678317b1d35b73f`: 292
  JavaScript-family files and 388 `route({ handler })` or handler-object
  matches.
- `taskforcesh/bullmq@ec2efcf4493479f9e7ecd0ae8a9000a831bb0084`: 171
  JavaScript-family files and 677 `new Worker(...)` matches.
- `nestjs/nest@983dd52c4927753be3421162fc43e4fde8d3fcde`: 1,675
  JavaScript-family files with 618 controller or constructor-injection matches.

The Fastify sample exposed a route-object handler gap. The BullMQ sample exposed
a constructor function-value gap. NestJS controller routes and TypeScript
constructor/property receiver calls were rechecked against existing parser and
reducer fixtures, so no new query policy was needed for that corpus class.

Reproduction commands:

```bash
git clone --depth 1 --filter=blob:none https://github.com/fastify/fastify.git /tmp/eshu-ts-corpus-336/fastify
git clone --depth 1 --filter=blob:none https://github.com/taskforcesh/bullmq.git /tmp/eshu-ts-corpus-336/bullmq
git clone --depth 1 --filter=blob:none https://github.com/nestjs/nest.git /tmp/eshu-ts-corpus-336/nest
git -C /tmp/eshu-ts-corpus-336/fastify fetch --depth 1 origin 1c49974805e57a17f1616f422678317b1d35b73f
git -C /tmp/eshu-ts-corpus-336/fastify checkout --detach 1c49974805e57a17f1616f422678317b1d35b73f
git -C /tmp/eshu-ts-corpus-336/bullmq fetch --depth 1 origin ec2efcf4493479f9e7ecd0ae8a9000a831bb0084
git -C /tmp/eshu-ts-corpus-336/bullmq checkout --detach ec2efcf4493479f9e7ecd0ae8a9000a831bb0084
git -C /tmp/eshu-ts-corpus-336/nest fetch --depth 1 origin 983dd52c4927753be3421162fc43e4fde8d3fcde
git -C /tmp/eshu-ts-corpus-336/nest checkout --detach 983dd52c4927753be3421162fc43e4fde8d3fcde
rg --files /tmp/eshu-ts-corpus-336/fastify | rg '\.(mjs|cjs|js|jsx|ts|tsx)$' | wc -l
rg -n 'route\s*\(\s*\{|handler\s*:' /tmp/eshu-ts-corpus-336/fastify --glob '*.{mjs,cjs,js,jsx,ts,tsx}' | wc -l
rg --files /tmp/eshu-ts-corpus-336/bullmq | rg '\.(mjs|cjs|js|jsx|ts|tsx)$' | wc -l
rg -n 'new\s+Worker\s*\(' /tmp/eshu-ts-corpus-336/bullmq --glob '*.{mjs,cjs,js,jsx,ts,tsx}' | wc -l
rg --files /tmp/eshu-ts-corpus-336/nest | rg '\.(mjs|cjs|js|jsx|ts|tsx)$' | wc -l
rg -n '@Controller|constructor\s*\(' /tmp/eshu-ts-corpus-336/nest --glob '*.{mjs,cjs,js,jsx,ts,tsx}' | wc -l
```

Eshu now models Fastify route-object handler roots and emits function-value
references for values passed through route object `handler` properties and
constructor arguments such as BullMQ processors.

This branch does not promote TypeScript or TSX candidates to cleanup-ready.
Full command/API returned-candidate sampling and manual precision labeling
remain part of issue #336 before TypeScript can claim exact cleanup safety.

## Fastify Declaration API Sampling

Issue #336 follow-up sampling used the same pinned Fastify checkout,
`fastify/fastify@1c49974805e57a17f1616f422678317b1d35b73f`, through a fresh
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

Accuracy result: Fastify TypeScript precision for this sample is 5/5 false
positive ambiguous candidates removed, with zero cleanup-ready TypeScript
candidates emitted. TypeScript remains `derived`, not exact cleanup-safe truth.

Performance Evidence: the final Fastify local-authoritative proof indexed 324
files and 8,274 content entities with collector discovery at 0.0275 s,
pre-scan at 1.4795 s, parse wall time at 1.3065 s, TypeScript cumulative parse
time at 6.8418 s across 35 files, materialization at 0.0845 s, collector stream
wall time at 2.9154 s, content write at 0.5262 s, and code-call projection at
0.4243 s. Queue terminal state was pending=0, in_flight=0, retrying=0,
dead_letter=0, failed=0.

No-Regression Evidence: focused TDD failures for Fastify route-object handlers
and constructor function-value references were added first. After the parser
evidence fix, `go test ./internal/parser -run
'TestDefaultEngineParsePathTypeScriptFastifyRouteObjectHandler' -count=1` and
`go test ./internal/reducer -run
'TestExtractCodeCallRowsResolvesFastifyRouteObjectHandlerReference|TestExtractCodeCallRowsResolvesConstructorFunctionValueReference'
-count=1` passed.

Benchmark Evidence: on Apple M1 Max / darwin arm64, `go test
./internal/reducer -run '^$' -bench
'BenchmarkExtractCodeCallRowsLargeJavaScriptDynamicCalls|BenchmarkResolveDynamicJavaScriptCallee'
-benchmem -count=1` measured the large dynamic-call benchmark at 8.729 ms/op,
1,643,794 B/op, and 29,699 allocs/op on `origin/main`. A final isolated
`-count=3` branch run measured the same benchmark at 8.616 ms/op, 8.687 ms/op,
and 8.640 ms/op with 29,699 to 29,700 allocs/op. The dynamic JavaScript resolver
microbenchmarks stayed within run noise: anonymous-source fallback moved 1,484
ns/op to about 1,472 to 1,483 ns/op, and no-alias fallback moved 1,179 ns/op to
about 1,166 to 1,197 ns/op.

Observability Evidence: no new read-path query or worker stage was added. The
existing code-call materialization completion log reports `fact_count`,
`repo_count`, `code_call_row_count`, `intent_row_count`,
`extract_duration_seconds`, `build_intents_duration_seconds`,
`upsert_intents_duration_seconds`, and `total_duration_seconds`. The existing
bounded dead-code investigation coverage reports `candidate_scan_pages`,
`candidate_scan_rows`, `candidate_scan_limit`, and truncation fields.
