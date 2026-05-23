# internal/relationships Agent Instructions

These rules are mandatory for this package. Root `AGENTS.md` still owns the
repo-wide proof, performance, concurrency, and skill-routing rules.

## Read First

1. `README.md` and `doc.go`.
2. `models.go`, `evidence.go`, and `resolver.go`.
3. The extractor file for the family you touch: Terraform, provider schema,
   Terragrunt, Helm, Kustomize, Argo CD, CI, Ansible, Dockerfile, or Compose.
4. `terraform_schema.go` and `go/internal/terraformschema/README.md` before
   changing schema-driven Terraform evidence.
5. `go/internal/reducer/README.md` before changing admission-facing behavior.

## Local Rules

- This package reports evidence and candidates only. It does not write graph
  edges, enqueue work, persist rows, or decide canonical truth.
- Extractors must be deterministic for the same facts, catalog aliases, and
  schema inputs.
- Evidence must trace to concrete source content, matched aliases, and
  rationale. Do not invent deployment truth from namespace, folder, or repo-name
  heuristics.
- Ambiguous signals stay low-confidence unless explicit assertions or stronger
  evidence admit them.
- Do not lower `DefaultConfidenceThreshold` or inflate extractor confidence to
  force materialization.
- All catalog alias matching must go through the package matching path so
  deduplication and rationale stay consistent.
- Terraform registry source strings are not repository aliases by themselves.
- Argo CD multi-source extraction must preserve source/path/root/revision tuple
  alignment by source index.
- ApplicationSet extraction needs generator files in the same envelope batch.
- Keep schema-driven Terraform extractor registration nil-safe and optional;
  missing schemas must not make ordinary evidence extraction fatal.

## Change Gates

- New extractors need positive, negative, and ambiguous fixtures, plus
  deterministic ordering tests.
- New evidence kinds or model fields require storage/reducer/API compatibility
  review when they are persisted or surfaced.
- Schema-driven Terraform changes require tests in both
  `internal/relationships` and `internal/terraformschema` when provider schema
  behavior changes.

## Do Not Change Without Owner Review

- `DefaultConfidenceThreshold`.
- Persisted evidence kind strings.
- Assertion decision strings and resolver admission contract.
- Package boundary: no storage, queue, graph, or reducer imports.
