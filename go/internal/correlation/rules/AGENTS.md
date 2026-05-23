# internal/correlation/rules Agent Rules

This package is metadata only: schema validation and first-party rule-pack
constructors. It MUST NOT evaluate candidates, perform admission, render
explain output, carry candidate data, or import runtime/storage packages.

## Read First

MUST read these before editing:

1. `README.md` and `doc.go`.
2. `schema.go` and `container_rulepacks.go`.
3. The specific `*_rules.go` file being changed.
4. `../engine/README.md` and `../admission/README.md`.
5. Root correlation truth gates before changing thresholds or structural
   requirements.

## Local Invariants

- `RulePack.Validate` MUST run before engine consumption; invalid packs produce
  no partial results.
- Engine ordering is `(Priority, Name)`. Rule names and priorities are therefore
  explain/output contracts.
- `MinAdmissionConfidence` is a hard correctness gate, not a tuning knob.
- `EvidenceRequirement.MinCount` MUST be positive. Optional evidence is modeled
  by omitting a requirement.
- `MatchAll` is conjunctive against one atom. Do not treat selectors as OR.
- `MaxMatches == 0` means unbounded, not disallowed.
- Pack constructors MUST return fresh value slices. Do not share mutable rule or
  requirement slices across packs.
- Do not infer environment, platform, cluster, or service truth from namespace,
  folder, or repo-name strings.

## Change Rules

- New first-party pack: add a constructor, add it to `FirstPartyRulePacks`, add
  it to `ContainerRulePacks` only if it belongs to that family, and update
  reducer/query/docs for any visible result family.
- New `RuleKind` requires schema validation plus engine handling when it is
  intended to affect match counts.
- New `EvidenceField` requires schema validation plus
  `admission.evidenceFieldValue` dispatch.
- Threshold or structural requirement changes MUST include positive, negative,
  and ambiguous proof for admitted and rejected populations.

## Proof

Run the focused gate for any edit:

```bash
cd go
go test ./internal/correlation/rules -count=1
go vet ./internal/correlation/rules
go doc ./internal/correlation/rules
```

Admission-shape changes require `go test ./internal/correlation/... -count=1`.
Docs-only edits also need the package-doc verifier for this directory and
`git diff --check`.
