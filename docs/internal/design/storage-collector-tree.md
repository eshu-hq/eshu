# Storage and collector target tree (#6692)

Owner-approved destination for the `go/internal/storage` and
`go/internal/collector` restructure. Versioned here from issue #6692 so the
plan is reviewed and pinned, not only described in an issue. Where this
document conflicts with anything older, the current decisions in #6692, #6061,
and #6707 win.

Status: approved direction. No file moves land in this PR.

## Ecosystem destination

Every non-default collector will move to its own repository. Eshu retains
default Git collection and parser/reducer core. This plan prepares those
boundaries while preserving behavior.

Each moved package must identify its current owner, destination owner, private
implementation dependencies, and public contracts. Distinguish collectors from
shared fact formats, Git support, conformance tooling, and runtime
infrastructure. Provider resource packages move with their collector.

Storage packages follow their state and runtime owners. Nesting SQL or Cypher
code does not create a database service or grant extracted collectors database
access.

Where historical rules say every path level is a future repository, this
section supersedes them: nest concepts when the hierarchy clarifies ownership;
the ecosystem inventory in #6707 defines repository and runtime boundaries.

## Why

#6053 set out to make every Go package directory hold at most 40 non-test files. Its
workstreams covered query, reducer, projector, coordinator, mcp, parser and one
collector package. The directory gate still lists four large storage and collector
directories that no issue owned:

| directory | non-test files |
| --- | ---: |
| `internal/storage/postgres` | 372 |
| `internal/collector/awscloud` | 154 |
| `internal/storage/cypher` | 131 |
| `internal/collector/gcpcloud` | 96 |

`storage/postgres` alone is larger than `internal/query` was when query was
considered the worst directory in the repo. #6053 has been narrowed to the
packages it tracks, and this epic owns storage and collectors.

The goal is the same as #6053's, and the reason is readability first. A contributor
should be able to open `internal/collector` or `internal/storage/postgres` and find
what they want by reading directory names, not by scrolling a flat list of hundreds
of files.

## The rules, and they are binding

`docs/internal/naming.md`, plus the Package And File Names section of the
golang-engineering skill:

1. Plain English at every path level.
2. **Never repeat the directory name in the file name.** `correlation/writer.go`,
   not `correlation/correlation_writer.go`.
3. **Split compound names into nested directories; never glue full names.** Each
   level clarifies responsibility. Repository and runtime boundaries come from
   the ecosystem inventory in #6707.
4. Follow Effective Go: no package-name stutter in exported identifiers.
5. A move leaves names better than it found them.

Every directory ends at 40 non-test files or fewer.

## Owner decisions, settled

1. **The Prometheus/Mimir collector is a Prometheus collector.** It calls only the
   standard Prometheus HTTP API (`/api/v1/targets`, `/api/v1/rules`) and reaches
   Mimir through the `X-Scope-OrgID` tenant header. Path:
   `collector/observability/prometheus`. Mimir support is described in the package
   doc, not the path. Coordinator's `planner/metrics` becomes `planner/prometheus` so
   the two match (tracked on #6627).
2. **The 133 `awscloud/constants_<service>.go` files consolidate into about 12
   files in one package, `collector/cloud/aws/catalog`,** grouped by service category
   (`compute.go`, `storage.go`, `database.go` and so on). Each file today holds one
   small `const` block.
3. **`awscloud/services/` becomes `cloud/aws/service/`**, singular per Go convention.
4. **The shared database interfaces in `storage/postgres/db.go` move to
   `storage/postgres/db`.** Not `sql`, which would collide with the standard
   library's `database/sql`.
5. **`collector/secretsiam` is shared source-fact support, not a collector runtime.**
   It makes no API calls. During in-tree naming cleanup it becomes
   `collector/access/posture`; its public fact shapes move to `eshu-sdk`, while
   provider acquisition follows each collector repository. The fact kind
   `secrets_iam_posture` remains unchanged. Effective-access reduction stays in
   Eshu core under `reducer/secrets/iam`; projector and MCP paths follow their
   distinct responsibilities instead of copying this package path (see #6061
   and #6627).
6. **`collector/contracttest` and `collector/parity` move to
   `collector/conformance/{contract, parity}`.** Both are test harnesses, not data
   sources. `parity` has zero importers, so read it before moving it: it is either
   self-tested or dead.

## Import cycles decide the shape, so check them first

**AWS, measured.** 1,224 files under `awscloud/services/` import the awscloud root,
and the root itself uses the `Service*` constants. Moving each constant file next to
its service would create a cycle between root and service. That is why the constants
become a leaf `catalog` package that imports neither side.

**gitrepo, already proven by #6056.** No leaf emitter can be moved alone, because
each needs the fact-stream writer that the fact stream also calls. The shared half is
`gitrepo/gitmodel`, and imports run one way: `gitrepo -> leaf -> gitmodel`. Renaming
the directory to `repo/git` keeps that direction and is mechanical. Getting below
66 files needs new cycle analysis.

**GCP extractors and Postgres stores, not yet measured.** Both plans hoist their
shared types into a leaf, so they work whichever way the imports run. Each move still
verifies with `go/types` before starting.

## Target tree: `internal/collector`

```text
collector/
├── cloud/
│   ├── aws/                    ← awscloud: about 21 root files after catalog moves out
│   │   ├── catalog/            ← the 133 constants_*.go, as about 12 files by category
│   │   └── service/<service>/  ← services/, renamed (134 existing packages)
│   ├── azure/                  ← azurecloud (17)
│   └── gcp/                    ← gcpcloud: 16 root files
│       └── extractor/
│           ├── compute/        ← about 19: instance, network, firewall, route, vpn_tunnel …
│           ├── data/           ← about 18: bigquery_table, spanner_database, sql_instance …
│           ├── security/       ← about 14: iam_role, kms_key_ring, service_account …
│           ├── serverless/     ← about 11: run_service, cloud_function, eventarc_trigger …
│           └── platform/       ← about 15: artifact registry, pubsub, dns, logging, firebase, gke, storage
├── repo/
│   ├── git/                    ← gitrepo (66); gitmodel becomes git/model
│   ├── discovery/              ← discovery (repo-root file sets, .gitignore/.eshuignore)
│   ├── submodule/
│   └── codeowners/
├── access/posture/             ← secretsiam
├── preflight/{archive, diagram, image, media, ooxml, pdf, manifest}
├── document/{media, ocr, export}          ← mediadoc, ocrdoc, documentationexport
├── observability/{grafana, loki, tempo, prometheus}
├── sbom/{document, runtime}
├── terraform/state/            ← terraformstate (38)
│   └── runtime/                ← tfstateruntime (7)
├── registry/{oci, package}
├── vulnerability/{intelligence, ospackage}
├── security/alerts/            ← securityalerts
├── kubernetes/live/  vault/live/  cicd/run/  service/catalog/
├── confluence/  jira/  pagerduty/
├── conformance/{contract, parity}
└── extension/host/  scanner/worker/  entrypoint/  sdk/
```

File names lose the prefix their directory now carries:
`gcpcloud/extractor_compute_instance.go` becomes
`cloud/gcp/extractor/compute/instance.go`.

## Target tree: `internal/storage/postgres`

```text
storage/postgres/
├── db/                         ← db.go: ExecQueryer, Transaction, Beginner, SQLDB
├── identity/{local, provider, bootstrap, api, saml, admin, sign, session}
├── search/{document, index, vector}      ← the 17 eshu_search_* files
├── facts/                      ← facts_active (16) + schema_fact (9)
├── ingestion/                  ← ingestion_backfill (12) + ingestion_reopen (4)
├── queue/{reducer, projector}
├── cloud/{aws, gcp, multi, inventory}
├── terraform/state/{drift, backend}
├── content/  workflow/  intent/  tenant/  semantic/
├── container/image/  crossplane/  supply/chain/  incident/  webhook/
├── generation/  maintenance/  scope/
└── migrations/  array/  rebuild/reset/   ← pgarray repeats its parent; rebuildreset was glued
```

`eshu_search_vector_metadata.go` becomes `search/vector/metadata.go`: the repository
name is not a useful prefix inside the repository. `identity_saml_provider.go` becomes
`identity/saml/provider.go`.

`identity` holds more than 40 files, which is why it splits. Every other domain
listed is under the cap.

## Target tree: `internal/storage/cypher`

```text
storage/cypher/
├── edge/{writer, materialized}           ← edge_writer (16) + materialized_edge (3)
├── canonical/{node, inheritance, flux, terraform}   ← the 34 canonical_* files
├── cloud/
│   ├── resource/                        ← shared cloud node writer and existence contract
│   ├── aws/{relationship,image,ec2,iam,rds,s3,security/group}/
│   ├── azure/relationship/
│   └── gcp/relationship/
├── crossplane/satisfaction/
├── sweep/orphan/  fault/executor/  phase/group/
└── semantic/entity/  package/registry/
```

Provider-specific Cypher writers follow the reducer and projector under
`cloud/<provider>/`. Only genuinely shared cloud code stays at `cloud/resource/`;
Crossplane stays outside cloud; the Lambda container-image writer is AWS-specific under `cloud/aws/image/`.
These remain private Eshu core writers, not collector implementations or
independent storage services. Split shared helpers and tests at a narrow
cycle-free seam before moving leaves; preserve every Cypher statement and
writer contract.

`storage/nornicdb` (11) and `storage/neo4j` (1) are under the cap and stay as they are.

## Ground rules

- **The first PR commits this plan as
  `docs/internal/design/storage-collector-tree.md`**, before any file moves, so the
  plan is versioned and not only in an issue.
- **Membership is verified by symbol, never by prefix.** A census in
  `internal/query` found 4 of 46 `code*.go` files belonged to a different handler.
  Every move re-derives its family with `go/types` first.
- **Where a name here reads worse than what the package actually does, say so on the
  child issue with what you read, and get a yes before moving.** These names came from
  directory listings, file counts and a handful of package docs, not a full read of
  every package.
- **Default to one coherent owner closure per commit or PR.** A reviewer confirms
  names moved, imports updated, and behavior preserved. The earlier #6634
  exception is historical and superseded; storage and collector naming belongs
  to the child issues above, in separate reviewable owner-closure PRs.

## Mandatory proof requirements (every PR under this epic)

- **Moves change no behaviour.** `git mv` so history follows; whole-module build and
  vet; the full recursive test run for the touched tree; B-7 and B-12 byte-identical.
  A move that changes projected truth is a bug.
- **Run the mandatory late `make pre-push` floor.** For package moves,
  `make pre-pr-full` is a recommended but optional deeper gate. When it is not
  run, focused whole-module build, vet, race, importer, and contract proof must
  ensure CI is not the first test of moved wiring.
- **Prove test repoints with `go test -list`.** A stale `-run` pattern selects zero
  tests and still exits 0.
- **Gate and spec lockstep in the same PR.** Any CI trigger, verify-script, ledger or
  generated artifact that names a moved path is updated with the move. `rg` the old
  path across the repo, including `scripts/`, `.github/workflows/`, `specs/` and
  `docs/`, before a PR is ready.
- **Every new directory carries `doc.go`, `README.md` and `AGENTS.md`** with real
  content.
- **The dirgate ledger is re-derived, never hand-edited.** On every sibling merge,
  take `origin/main`'s copy of the ledger and its generated mirror, recompute count
  and digest for the real tree, and check the arithmetic. A clean line-merge of that
  file is the dangerous case.
- **Watch for new security findings.** Moving the openapi files out of `internal/query`
  let gosec analyse the smaller package, and it found a live open redirect. Smaller
  packages expose findings that were always there. Fix them where they surface.
- Capture exit codes directly, never after a pipe. `eshu-code-review` at
  P0=P1=P2-blocking=0 before the preflight and again on the post-preflight diff.

## Order

1. The design-doc PR.
2. **`storage/postgres`**: hoist `db/` first, then domains, `identity` last because it
   splits.
3. **`storage/cypher`**.
4. **`collector` (non-cloud)**: renames and nests, mostly mechanical.
5. **`collector/cloud`**: the AWS catalog consolidation, the `service/` rename and the
   GCP extractor grouping. This comes last because it touches the most packages.

## Workstreams

- #6693 — restructure: storage/postgres — hoist db, then split by domain
- #6694 — restructure: storage/cypher — nest edge, canonical and domain writers
- #6695 — restructure: collector — nest the non-cloud collectors under plain-English parents
- #6696 — restructure: collector/cloud — aws catalog and service rename, gcp extractor grouping, azure
