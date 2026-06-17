# AGENTS.md — internal/auditpreflight guidance for LLM assistants

## Read first

1. `go/internal/auditpreflight/README.md` — the preflight contract and taxonomy.
2. `go/internal/auditpreflight/auditpreflight.go` — `ParseIssue`, `Validate`, and
   the `GapClasses`/`OwnerSurfaces` taxonomy.
3. `.github/ISSUE_TEMPLATE/competitive-audit.yml` — the issue form whose labels
   must match `RequiredFields` headings.
4. `docs/public/reference/competitive-audit-preflight.md` — the workflow.

## Invariants this package enforces

- **Headings are a contract.** `RequiredFields` headings must match the issue
  form field labels exactly (GitHub renders a field label as `### label`).
  Changing one without the other breaks validation.
- **Closed taxonomy.** Gap class and owner surface are validated against
  `GapClasses` and `OwnerSurfaces`. Add a value in both the taxonomy and the
  issue-form dropdown, never only one.
- **Deterministic findings.** `Validate` sorts findings; keep new checks sorted.
- **No I/O.** The package is pure; the command does file/stdin reads.

## Common changes and how to scope them

- **New required section** → add to `RequiredFields`, add the matching field to
  the issue form, add a test. Why: the form and validator are co-owned.
- **New gap class / owner surface** → extend the taxonomy slice and the
  issue-form dropdown options; add a passing fixture.

## Failure modes and how to debug

- Symptom: a valid-looking issue fails with `missing_field` → cause: the issue
  heading text drifted from `RequiredFields` → align the form label and heading.
- Symptom: a dropdown value fails `invalid_gap_class` → cause: the option text in
  the form is not in `GapClasses` → align them.

## What NOT to change without an ADR

- The taxonomy vocabulary — it is shared with the audit report generator (#2716).
