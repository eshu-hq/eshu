# Cassette And Replay Proof

Eshu replay is the deterministic proof layer for collector output, parser
fixtures, replay scenarios, and API/MCP/CLI answer shapes. It lets contributors
and maintainers prove supported behavior without provider credentials, Docker,
Postgres, or a graph backend unless the named proof gate explicitly says a live
backend is part of the contract.

Use this page when you need to author, refresh, validate, or review a replay
scenario, or when you need to understand what a green replay or cassette result
actually proves.

## The No-Provider-Key Rule

Ordinary replay proof is credential-free. A normal validation run must not read
cloud tokens, API keys, customer endpoints, private hostnames, or local ignored
configuration. A replay input that cannot run without provider keys belongs in a
credentialed refresh job, not in an ordinary proof gate.

The split is deliberate:

- **Record or refresh** may call a live provider, using operator-owned
  credentials, to capture source behavior once.
- **Replay and validation** use committed, canonical, redacted artifacts and
  must run offline with no live provider access.

If a replay misses a request, fact kind, parser fixture, or query shape, it must
fail loudly. It must not fall through to the network, silently skip a surface, or
turn an unknown artifact into a passing result.

## Contributor Conformance Flow

For collector extraction conformance, start with the public, Docker-free
conformance suite. These commands are the runnable starter path; the package
README has the implementation details.

```bash
git clone https://github.com/eshu-hq/eshu && cd eshu/go
go test ./conformance -count=1
$EDITOR conformance/testdata/starter-spec.yaml
go run ./cmd/collector-<record-capable-collector> -mode=record \
  -cassette-file=conformance/testdata/starter-cassette.json
$EDITOR conformance/observe.go
go test ./conformance -count=1
```

The first and last commands are credential-free. The `-mode=record` command is
the optional live step: it runs your collector once against your source system,
writes a canonical cassette, and does not require Postgres, NornicDB, or Docker.
Use a binary that actually implements record mode. The in-tree pilots are
`collector-kubernetes-live` (raw) and `collector-aws-cloud` (pseudonymized, see
[Record-Mode Pseudonymization](#record-mode-pseudonymization)); out-of-tree
collectors should substitute their own record-capable binary after adding the
same recorder seam.

After recording real collector facts, update `conformance/observe.go` so
`Observe` maps those fact kinds into the node, edge, correlation, property, and
evidence observations expected by `conformance/testdata/starter-spec.yaml`.
The starter `Observe` seam intentionally rejects unknown fact kinds; leaving it
on the neutral `starter.*` mapping makes the final conformance test fail against
any real collector cassette.

## Replay Artifact Types

| Artifact | What it proves | Where to start |
| --- | --- | --- |
| Collector cassette | A credentialed collector's emitted facts can be replayed as if the collector ran live. | `testdata/cassettes/<collector>/<recording>.json` |
| Input tape | An HTTP-backed collector can replay provider responses through its real parsing and normalization code. | `go/internal/replay/inputtape` |
| Parser fixture | A parser emits the expected facts for a committed source tree. | `go/internal/replay/parserfixture/testdata/fixtures` |
| API/MCP golden | A graph-backed HTTP or MCP read returns the expected bounded response shape. | `testdata/golden/e2e-20repo-snapshot.json` |
| CLI golden | A CLI read surface matches the same offline answer contract as shared read surfaces. | `query_shapes.cli` in the B-12 snapshot |
| Authz scoped route | In-grant and out-of-grant scoped token behavior matches the authorization catalog. | `specs/authorization-replay-coverage.v1.yaml` |
| Scenario-depth proof | A required surface has baseline plus any applicable `delta_tombstone`, `fault`, `ordering`, `crash`, or `cost` coverage. | `specs/replay-coverage-manifest.v1.yaml` |

The generated [Replay Coverage Dashboard](replay-coverage.md) shows which
surfaces and scenario-depth classes are covered, what artifact type covers each
one, and which sibling gate proves it green.

## Authoring A Scenario

1. Pick the surface key from the source-of-truth registry:
   `collector:<name>`, `read_surface:<route>`, `cli_surface:<command>`,
   `parser:<name>`, `capability:<id>`, `product_claim:<id>`, or
   `authz_family:<family>:<mode>`.
2. Choose the scenario type. Every supported surface needs `baseline`; add
   `delta_tombstone`, `fault`, `ordering`, `crash`, or `cost` only when the
   surface's behavior depends on that depth class.
3. Add or update the artifact that actually exercises the behavior. Do not add a
   manifest row that points at a future or placeholder proof.
4. Add the manifest row in `specs/replay-coverage-manifest.v1.yaml`, including
   `surface`, `scenario`, `scenario_type`, `ref`, and `proof_gate`.
5. Run the gate that proves the artifact green, then run the replay coverage
   gate so the dashboard and coverage report remain in lockstep.

The `proof_gate` field is not decoration. It names the command or CI gate that
actually runs the scenario. A manifest row without a real sibling proof is a
false green.

`proof_gate` values are statically validated against `specs/ci-gates.v1.yaml`.
Every value used by `specs/replay-coverage-manifest.v1.yaml` or
`specs/authorization-replay-coverage.v1.yaml` must name a registered gate with a
local command and a CI workflow, or a registered local-only gate with an explicit
`local_only_reason`. A typo, deleted gate, or gate with no runnable local command
fails `replay-coverage-gate` before the entry can be counted as covered. The
replay coverage workflow and CI-gate registry also watch the files that can
invalidate each scenario class: cassettes, parser fixtures, B-12 snapshots,
API/MCP/CLI golden code, authz catalogs, capability/product-claim specs, the
manifest, and scenario-depth artifacts.

## Refreshing A Cassette

Refresh only when source behavior changed, a collector fact contract changed, or
a committed cassette no longer represents the intended synthetic source. Do not
refresh to hide a failing proof.

Collector record mode is symmetric with cassette replay mode only for binaries
that wire the replay recorder. Do not assume every collector supports
`-mode=record`; add the recorder seam first, use the existing credentialed
refresh workflow for supported recorders, or treat the cassette update as
blocked until a supported recorder exists.

```bash
go run ./cmd/collector-<record-capable-collector> -mode=record \
  -cassette-file=../testdata/cassettes/<collector>/<recording>.json
```

For the current in-tree pilots, that is:

```bash
go run ./cmd/collector-kubernetes-live -mode=record \
  -cassette-file=../testdata/cassettes/kuberneteslive/supply-chain-demo.json
ESHU_RECORD_PSEUDONYM_KEY="$(cat /path/to/corpus.key)" \
  go run ./cmd/collector-aws-cloud -mode=record \
  -cassette-file=../testdata/cassettes/awscloud/supply-chain-demo.json
```

### Record-Mode Pseudonymization

A recording of a real estate carries account ids, ARNs, resource names,
hostnames and addresses, none of which may be committed. Recorders that
implement it (`collector-aws-cloud` is the pilot) rewrite every identifier
before the recorder sees it, through `go/internal/replay/recordpseudo`:
each raw token becomes a keyed pseudonym of the same shape (12-digit accounts
in the reserved `0000` range, ARNs with their grammar intact, `n` + 11 hex
names, RFC 5737 addresses), so the reducers project the same graph from the
recording as from the raw run. Equal raw tokens under one key give equal
pseudonyms, which is what keeps cross-cassette joins alive.

The key is `ESHU_RECORD_PSEUDONYM_KEY`:

- generate it with `openssl rand -hex 32`;
- use **one key per corpus**: every cassette whose facts join each other
  (for example the AWS, OCI registry and Terraform-state cassettes of one
  demo estate) must be recorded under the same key, or the joins break;
- keep it with the corpus's other secrets and never commit it; the cassette
  carries only its 8-character fingerprint in `pseudonym_key_fingerprint`.

A recorder that pseudonymizes refuses to run without the key and refuses to
write a file the private-data gate would reject: `recordpseudo.Verify` scans
the canonical bytes with the gate's own alternatives before the file exists,
and admits the reserved `0000` account form only when that run minted it.
Payload field paths and scope metadata keys the collector's policy does not
classify are made opaque and listed in the `collector.record.pseudonymized`
log event, never their values. The composite envelope fields (`scope_id`,
`partition_key`, `stable_fact_key`, `source_record_id`, `source_uri`) are
rewritten by token substitution only, because the collector composes them
from tokens that also appear in classified fields, from one-way hashes or
from structural URI text; the recorder's `Verify` scan is the belt for them.

Declared limits of the pilot:

- two private CIDRs lose their overlap relation, and resource names lose
  readability;
- any customer host under an AWS-owned suffix -- an ELB DNS name, an RDS or
  OpenSearch endpoint, a Route53 alias target, an AWS service principal --
  is pseudonymized to a form (`h<hex>.<region>.<service>.amazonaws.com`) the
  private-data gate has no allow row for, so the recording is refused, not
  written, until a reviewed row exists;
- Keep-class free text (an environment name, an engine or status string)
  is written verbatim when it is not also learned from a classified field,
  and the recorder's belt carries no organisation identifiers. Run
  `scripts/verify-cassette-author.sh` with `ESHU_PRIVATE_IDENTIFIERS_FILE`
  set before committing a recording: that alternative is the check for
  this residual. Container image tags are not in this class: a
  customer-chosen tag is pseudonymized, and only `latest`, pure semver
  (`v1.2.3`, `1.2.3`) and digests are kept;
- one recording holds at most 762 distinct IPv4 addresses and CIDR
  networks (the RFC 5737 slot space). The 763rd makes the record run fail
  with `recording exceeds 762 distinct IPv4 addresses`, and no cassette is
  written; the `collector.record.pseudonymized` event reports
  `ipv4_addresses` so an operator can see how close a recording is;
- a name or tag value shorter than four characters, or a purely numeric
  name or tag value, is rewritten only where it is a whole field value or
  a whole `/`- or `:`-delimited component of a composite (an ARN, a stable
  key, a source uri), never inside longer text (otherwise a tag value `1`
  would rewrite every `us-east-1`) and never in a Keep field; one glued
  into a longer word without such a boundary stays raw, and a Keep value
  equal to one is kept;
- a numeric name or tag value of fewer than four digits is kept; a longer
  numeric name is rewritten wherever it is the resource name, including
  after a `:`-joined type token, and never rewrites an ARN qualifier after
  the name;
- a name or tag value that exactly matches the AWS region or
  availability-zone grammar (`us-east-1`, `eu-central-1a`) or an AWS
  service, resource-type or host-service word (`rds`, `db`, `iam`, ...) is
  AWS vocabulary and is kept.

Before committing a refreshed cassette:

- review the diff line by line; canonical output should make meaningful changes
  small and readable
- confirm volatile fields normalized instead of churning the whole file
- confirm secrets, tokens, private URLs, hostnames, IP addresses, and real
  account identifiers are absent, and run `scripts/verify-cassette-author.sh`,
  whose private-data scan is the gate behind that rule (see
  [Private-Data Gate](#private-data-gate))
- run the proof gate named by the manifest entry
- regenerate any generated dashboard or report only through the owning gate

The credentialed refresh workflow is separate from ordinary proof. It may use
provider secrets to re-record artifacts, but the resulting PR still needs
offline replay validation before merge.

## Private-Data Gate

Cassettes carry synthetic or redacted values only, and the `cassette-author`
gate (`scripts/verify-cassette-author.sh`, blocking, in the pre-push floor)
enforces it over every file under `testdata/cassettes/`. Seven alternatives are
scanned; a match is a candidate, and it passes only when it matches that
alternative's committed allowlist, so an unlisted value fails rather than
slipping past a blocklist:

| Alternative | Allowed forms |
| --- | --- |
| `ipv4` | RFC 5737 documentation ranges (`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`) and loopback |
| `nodeip` | EKS-style `ip-A-B-C-D` node names only in those documentation ranges or loopback |
| `ipv6` | RFC 3849 `2001:db8::/32` and `::1`; six-group MAC addresses also land here, and only the RFC 7042 documentation block (`00:00:5e:00:53:xx`) and the all-zero MAC pass |
| `account12` | `123456789012`, zero-prefixed `00000000000N`, repdigits, and the reserved `0000` + 8-digit range that record-mode pseudonymization mints accounts into (the recorder's own belt checks that a run minted them; here it is a shape check); twelve digits inside a hex digest are not a candidate |
| `arn` | an empty, `aws`, or documentation account field |
| `hostname` | reserved names (`.example`, `.test`, `.invalid`, `.localhost`, `example.com/.net/.org`; never `.local`, because `<svc>.<namespace>.svc.cluster.local` carries the namespace out), the exact public service hosts the corpus uses, `<service>.googleapis.com`, ECR under a documentation account, and the corpus's own `supply-chain-demo` synthetic zones |
| `identifier` | nothing; the committed canary `eshu-canary-org` plus every literal in `ESHU_PRIVATE_IDENTIFIERS_FILE` |

The hostname alternative is lexical: a dotted token is a candidate only when
its last label is in the gate's TLD list. The list covers the reserved names,
common generic TLDs, in-cluster `svc`, enterprise and infrastructure zones
(`.corp`, `.lan`, `.home`, `.intranet`, `.private`, `.consul`, `.aws`,
`.arpa`, `.edu`, `.gov`, `.mil`, `.int`), and the country TLDs that do not
collide with a file extension or a dotted code path in the corpus. A label
after a bare dot starts a candidate, so wildcard hosts such as
`*.apps.<zone>` are scanned. A domain under any TLD outside the list is a
blind spot of this scan. That includes the TLDs deliberately left out because
they collide with the corpus (`.in`, `.it`, `.is`, `.at`, `.no`, `.es`,
`.cc`, `.pl`, `.rs`, `.tf`, `.sh`, `.md`, `.ps`, `.pm`, `.so`, `.am`, `.mk`,
`.zip`, `.name`, `.email`, `.run`) and any other TLD not listed. That is why
recordings also pass through the collectors' redaction layer and an
independent re-validation before they are committed.

A finding is reported as `file:line:alternative`; the value is never printed.
The alternatives, the allowlist, and the controls that prove each still
detects its planted sample and still admits each allowed form live in
`scripts/lib/cassette_private_data_pattern.sh`. Widening the allowlist is a
reviewed edit to that file with a new negative-control sample.

Organisation and product identifiers never live in git. Point
`ESHU_PRIVATE_IDENTIFIERS_FILE` at a file of literals (one per line, `#`
comments allowed) to add them; when it is unset the scan says so on stderr and
runs with the canary alone, and `ESHU_PRIVATE_IDENTIFIERS_REQUIRED=1` turns
that into a failure. A configured file that is missing or empty always fails.

## Validation Commands

Use the smallest command that proves the touched surface, then run the local
pre-PR gate before pushing.

```bash
cd "$(git rev-parse --show-toplevel)"
(cd go && go test ./conformance -count=1)
(cd go && go test ./internal/replay/... -count=1)
(cd go && go test ./cmd/replay-coverage-gate ./internal/replaycoverage -count=1)
bash scripts/test-verify-replay-coverage-gate.sh
bash scripts/verify-replay-coverage-gate.sh --blocking
make pre-pr
```

For generated replay dashboard updates, refresh through the owning test:

```bash
cd "$(git rev-parse --show-toplevel)"
(cd go && go test ./cmd/replay-coverage-gate/ -update-dashboard)
```

For API/MCP/CLI golden truth, also run the golden-corpus gate commands named in
[Local Testing](local-testing.md#quick-verification-matrix).

## Advisory And Blocking Coverage

Replay coverage has two modes:

- **Advisory** reports uncovered, unresolved, or stale rows without failing the
  command. Use this for local exploration while designing a new surface.
- **Blocking** fails on uncovered, unresolved, or stale rows. CI uses blocking
  mode for supported surfaces, and local merge proof should use
  `bash scripts/verify-replay-coverage-gate.sh --blocking`.

Blocking coverage means the coverage map is complete and the named sibling gates
exist and are registered as enforceable proof gates. It does not mean every
possible live provider, backend, performance, or full-corpus behavior has been
exercised.

## What Replay Replaces

Replay can replace manual checks when the question is deterministic and already
represented by a committed artifact:

- collector fact-shape regression for a recorded source
- parser output for a committed fixture tree
- API/MCP/CLI response envelope shape for the B-12 snapshot corpus
- scoped-token allow/deny behavior listed in the authorization replay catalog
- schedule, fault, crash, delta/tombstone, and cost behavior that has a named
  replay scenario and proof gate
- public product or capability claims whose deterministic proof metadata points
  at committed registries, snapshots, or gates

In those cases, prefer the replay or golden gate over a manual click-through.
It is faster, repeatable, and reviewable in a PR diff.

## What Replay Does Not Replace

Replay does not replace proof whose correctness depends on live systems,
backend-specific behavior, corpus scale, or operator runtime state:

- provider credential validation, permission-hidden behavior, rate limits, or
  live API pagination
- real Postgres queue drain, lease contention, dead letters, or claim handoff
  unless the named proof gate is a live/backend gate
- NornicDB or Neo4j planner behavior, schema/index behavior, or hot-path
  performance when those are the subject of the change
- full-corpus latency, p95/p99 budgets, memory, CPU, pprof, or benchmark
  evidence
- Kubernetes, Helm, Docker Compose, hosted deployment, or remote E2E rollout
  proof
- telemetry quality for operators unless the changed path emits and validates
  the relevant metrics, spans, logs, status, or dashboards

When a PR claims one of these behaviors, name the irreducible live/backend,
scaled, or full-corpus gate in the PR evidence. Do not cite ordinary replay as a
substitute for a proof tier it cannot exercise.

## Review Checklist

Before merging a replay or cassette change, verify:

- the artifact is synthetic, redacted, canonical, and reviewable
- the source registry, manifest row, artifact, generated dashboard, and sibling
  proof gate agree
- `scenario_type` covers the actual risk depth, not only baseline existence
- API, MCP, and CLI shapes stay in parity when they share a read surface
- authz rows cover both in-grant and out-of-grant behavior when a permission
  family is in scope
- advisory language is not used to bypass a blocking supported-surface gap
- every skipped live, backend, scaled, or credentialed check is explicitly out
  of scope or routed to a tracked follow-up
