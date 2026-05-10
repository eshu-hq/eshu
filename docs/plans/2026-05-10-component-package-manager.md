# Component Package Manager Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add the first Eshu component package manager slice: manifest validation, local install registry, trust policy checks, CLI commands, and docs.

**Architecture:** The first slice is local and air-gapped first. It introduces an internal component package that validates manifests and manages a file-backed installed-component registry, then exposes it through `eshu component` commands. OCI pull and Sigstore verification remain future verifier backends; strict mode fails closed until those are implemented.

**Tech Stack:** Go, Cobra, YAML v3, JSON file registry, MkDocs.

---

### Task 1: Manifest And Trust Policy

**Files:**
- Create: `go/internal/component/manifest.go`
- Create: `go/internal/component/policy.go`
- Test: `go/internal/component/manifest_test.go`
- Test: `go/internal/component/policy_test.go`

**Step 1: Write failing tests**

Add tests for:
- loading a valid `ComponentPackage` manifest
- rejecting empty ID, publisher, version, type, and artifact digest
- rejecting invalid collector kind values
- allowing an allowlisted component
- rejecting disabled, revoked, and strict-mode packages

**Step 2: Run tests**

Run:

```bash
go test ./internal/component -count=1
```

Expected: fail because the package does not exist.

**Step 3: Implement manifest and policy**

Implement:
- `LoadManifest(path string) (Manifest, error)`
- `Manifest.Validate() error`
- `Policy.ValidateManifest(Manifest) VerificationResult`
- semantic version checks for package versions and compatible core ranges

**Step 4: Verify**

Run:

```bash
go test ./internal/component -count=1
```

Expected: pass.

### Task 2: Local Registry

**Files:**
- Create: `go/internal/component/registry.go`
- Test: `go/internal/component/registry_test.go`

**Step 1: Write failing tests**

Add tests for:
- installing a verified component writes registry state and manifest copy
- listing installed components returns stable order
- enabling and disabling activation records
- uninstalling active component fails
- uninstalling inactive component removes it

**Step 2: Run tests**

Run:

```bash
go test ./internal/component -run 'TestRegistry' -count=1
```

Expected: fail because registry APIs do not exist.

**Step 3: Implement registry**

Implement a file-backed registry under `<component-home>/registry.json` and
manifest copies under `<component-home>/packages/<id>/<version>/manifest.yaml`.

**Step 4: Verify**

Run:

```bash
go test ./internal/component -count=1
```

Expected: pass.

### Task 3: CLI Commands

**Files:**
- Create: `go/cmd/eshu/component.go`
- Test: `go/cmd/eshu/component_test.go`
- Modify: `docs/docs/reference/cli-reference.md`
- Modify: `docs/docs/reference/cli-indexing.md`
- Modify: `docs/docs/reference/cli-kiss.md`

**Step 1: Write failing CLI tests**

Add tests for:
- `runComponentInspect` prints manifest identity
- `runComponentInstall` installs into a temp component home
- `runComponentList` shows installed component
- `runComponentEnable` fails when component is not installed

**Step 2: Run tests**

Run:

```bash
go test ./cmd/eshu -run 'TestComponent' -count=1
```

Expected: fail because commands do not exist.

**Step 3: Implement commands**

Wire `eshu component` into `rootCmd`. Keep command logic thin and delegate to
`internal/component`.

**Step 4: Verify**

Run:

```bash
go test ./cmd/eshu ./internal/component -count=1
```

Expected: pass.

### Task 4: ADR And Reference Docs

**Files:**
- Create: `docs/docs/adrs/2026-05-10-component-package-manager-and-optional-collector-activation.md`
- Create: `docs/docs/reference/component-package-manager.md`
- Modify: `docs/mkdocs.yml`
- Modify: `docs/docs/architecture.md`
- Modify: `docs/docs/reference/fact-envelope-reference.md`
- Modify: `docs/docs/reference/plugin-trust-model.md`

**Step 1: Write docs**

Document:
- installed vs enabled vs claim-capable
- local package registry
- trust-policy modes
- CLI examples
- non-goals for first slice

Use the humanizer skill rules for clear, non-inflated prose.

**Step 2: Verify docs**

Run:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

Expected: pass.

### Task 5: Final Verification And PR

**Files:**
- All changed files

**Step 1: Run focused checks**

Run:

```bash
go test ./internal/component ./cmd/eshu -count=1
golangci-lint run ./internal/component ./cmd/eshu
git diff --check
```

Expected: pass.

**Step 2: Run full checks**

Run:

```bash
go test ./... -count=1
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

Expected: pass.

**Step 3: Request review**

Use `superpowers:requesting-code-review` before opening the PR.

**Step 4: Commit and push**

Use the `linuxdynasty` GitHub user before push.
