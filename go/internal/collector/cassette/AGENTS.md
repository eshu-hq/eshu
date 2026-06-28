# collector/cassette — agent scope

## Owned surface

- `go/internal/collector/cassette/` — the credential-free replay package.
- `testdata/cassettes/<collector>/` — the cassette JSON files this package reads.

## Key invariants

- `Source` MUST implement `collector.Source` and emit one `CollectedGeneration`
  per scope in document order, then return `ok=false` to signal batch exhaustion.
- Cassette files MUST contain only synthetic or anonymized data. NEVER commit
  live credentials, real account IDs, real IP addresses, or real hostnames.
- The cassette format version is `"1"`. Increment it (with a migration note) if
  breaking changes are required; do NOT silently change the shape.
- `LoadFile` MUST validate all required fields and return a descriptive error
  before returning a `Source`. Silent replays of malformed cassettes are a
  correctness defect.
- **`fact_kind` is replayed verbatim — no namespace transform.** `Source` sets
  the envelope `FactKind` to the JSON `fact_kind` exactly as written
  (`source.go`). A cassette fact therefore only reaches a reducer/projection if
  its `fact_kind` string is byte-equal to the constant that consumer matches on.
  Some families match an UNPREFIXED kind (e.g. the secrets/IAM reducer matches
  `facts.VaultAuthRoleFactKind == "vault_auth_role"`), so a cassette written with
  a namespaced `"secrets_iam.vault_auth_role"` is silently **inert** — no error,
  it just produces no nodes. When authoring a cassette, copy the exact `fact_kind`
  the consuming reducer matches and verify the projection actually emits nodes
  (the golden-corpus gate is the check); a 0-node result is usually a `fact_kind`
  or join-key mismatch, not a writer bug.
- **Cross-fact join keys are matched literally, not re-derived.** A cassette that
  links facts (e.g. a vault auth-role bound to a k8s service account) joins on
  string-equal keys (`service_account_join_key`, `role_join_key`,
  `token_policy_join_keys`, ...). Use consistent synthetic values
  (`"sha256:sa-<name>"`) across the related facts; any mismatch resolves to
  nothing. These keys are keyed-HMAC fingerprints in live collection but plain
  synthetic strings in cassettes — the reducer only checks equality, so they need
  only be self-consistent within the cassette set.

## Skill routing

- `golang-engineering` for any Go change to this package.
- `eshu-diagnostic-rigor` if you add telemetry or measure replay throughput.

## Do not

- Add network calls or SDK imports to this package.
- Add credentials, real hostnames, or private data to cassette files.
- Allow `LoadFile` to succeed when required fields are missing.
