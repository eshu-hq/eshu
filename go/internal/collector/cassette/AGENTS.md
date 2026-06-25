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

## Skill routing

- `golang-engineering` for any Go change to this package.
- `eshu-diagnostic-rigor` if you add telemetry or measure replay throughput.

## Do not

- Add network calls or SDK imports to this package.
- Add credentials, real hostnames, or private data to cassette files.
- Allow `LoadFile` to succeed when required fields are missing.
