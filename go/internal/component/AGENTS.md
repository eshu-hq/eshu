# internal/component Agent Instructions

These rules are mandatory for this package. Root `AGENTS.md` still owns the
repo-wide proof, performance, concurrency, and skill-routing rules.

## Read First

1. `README.md` and `doc.go`.
2. `manifest.go` before changing package metadata.
3. `policy.go` before changing trust or provenance behavior.
4. `registry.go` before changing install, activation, disable, or uninstall
   state.
5. `docs/public/reference/component-package-manager.md` before changing
   user-visible component behavior.

## Local Rules

- Installed does not mean enabled. Enabled does not mean claim-capable.
- Git remains built in; optional collectors and services must be installed and
  enabled explicitly.
- Trust policy fails closed when provenance cannot be verified.
- Registry writes must stay atomic so partial writes cannot corrupt
  `registry.json`.
- Component artifact images must stay digest-pinned.
- Manifests must declare non-unknown source-confidence values for emitted fact
  families.
- Unknown or unsupported package behavior must remain inert at install time.

## Change Gates

- Manifest contract changes require validation tests, CLI-facing docs, and
  compatibility review.
- Policy changes require positive and negative tests for trust mode, allowlist,
  revocation, compatible-core, and provenance behavior.
- Registry changes require tests for installed/enabled separation, activation
  conflicts, atomic persistence, and uninstall safety.

## Do Not Change Without Owner Review

- Manifest field names or persisted registry JSON shape.
- Digest pinning.
- The install/enable separation.
