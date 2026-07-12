<!-- docs-catalog
title: Prove A Change
description: Walks through a real edit that turns a hermetic Ifá gate red, then green, using the contract-layer test suite.
type: tutorial
audience: practitioner
time: 10 minutes
entrypoint: true
landing: false
-->

# Prove a change

This tutorial breaks a real conformance case on purpose, watches the exact
gate CI runs go red, fixes it, and watches it go green. Every command below
was actually run against this codebase. The edit is made, tested, and
reverted — nothing here gets committed.

You need a Go toolchain. No Docker, no Postgres, no graph backend.

## 1. Confirm the baseline is green

```bash
cd go
go test ./internal/ifa/... -run TestFalseGreen -count=1 -v
```

You should see four passing tests, including
`TestFalseGreenBaselineKustomizeSatisfiesRC29`. This test proves the honest
green case first: the cataloged `odu:kustomize-deploys-from` Odù carries a
Kustomize overlay fact, and the production evidence extractor resolves it to
a `DEPLOYS_FROM` edge carrying `KUSTOMIZE_RESOURCE_REFERENCE` evidence —
exactly what B-12's `rc-29` requires.

## 2. Break the fixture

Open `go/internal/ifa/catalog_seed.go` and find `kustomizeDeploysFromOdu`.
Change the fact's `relative_path`:

```go
"relative_path": "overlays/prod/kustomization.yaml",
```

to:

```go
"relative_path": "overlays/prod/notkustomization.yaml",
```

The production evidence extractor keys Kustomize classification off the
filename. Renaming it away from `kustomization.yaml` makes the same content
invisible to the extractor — a realistic mistake, not a contrived one.

## 3. Watch it go red

```bash
go test ./internal/ifa/... -run TestFalseGreen -count=1 -v
```

Two tests fail, and the failure message names exactly what broke:

```text
--- FAIL: TestFalseGreenBaselineKustomizeSatisfiesRC29 (0.02s)
    coverage_falsegreen_test.go:70: baseline rc-29/kustomize status = "unresolved",
    detail="odù \"odu:kustomize-deploys-from\": relationship DEPLOYS_FROM missing
    evidence kind(s) [KUSTOMIZE_RESOURCE_REFERENCE]; observed kind(s) on that
    relationship: []", want covered
--- FAIL: TestFalseGreenEvidenceSatisfiesSanityInversion (0.01s)
    coverage_falsegreen_test.go:183: EvidenceSatisfies(rc-29, kustomize) = false,
    detail="relationship DEPLOYS_FROM missing evidence kind(s)
    [KUSTOMIZE_RESOURCE_REFERENCE]; observed kind(s) on that relationship: []",
    want true
```

This is the same test `cd go && go test ./internal/ifa ./cmd/ifa -count=1`
runs — the exact local command behind the blocking `ifa-contract-layer` CI
gate. A PR with this change would fail CI with this same message, not a bare
non-zero exit.

Notice `TestFalseGreenWrongOduBreaksRC29` still passes. It is the deliberate
control on the other side of this same coverage check: binding `rc-29` to the
ArgoCD Odù instead of the Kustomize one must also fail, or the coverage gate
could not tell the two deploy-source verbs apart. Both directions of that
control still hold; only the Kustomize path you just broke went red.

## 4. Fix it

Revert the `relative_path` back to `overlays/prod/kustomization.yaml`.

## 5. Watch it go green again

```bash
go test ./internal/ifa/... ./cmd/ifa/... -count=1
```

Every package passes. Confirm nothing was left behind:

```bash
git diff --stat
```

Empty output — the fixture is back to its committed state.

## What just happened

You changed a fact payload the way a real Kustomize-parsing regression
might: content technically still present, but no longer classifiable by the
production extractor. The `ifa-contract-layer` gate caught it immediately,
named the missing evidence kind, and pointed at the exact assertion that
failed — no need to guess which correlation broke or debug a live graph.
This is the same workflow for any Odù: break it, read the message, fix it,
confirm green, then run `make prove` before opening a PR.

## Next

- [Debug a failing gate](../guides/debug-a-failing-gate.md) for the other
  four Ifá gates, which need Docker and take longer to reproduce locally.
- [Add an Odù](../guides/add-an-odu.md) to register your own conformance
  case instead of editing an existing one.
- [The Ifá conformance platform](../concepts/ifa-conformance-platform.md) for
  what the contract layer is proving and how it fits the other three layers.
