# #6693 D2: migrations package owns the embed

Baseline: `origin/main` `7c561440c`. Change: add
`go/internal/storage/postgres/migrations` (package `migrations`), which now
carries `//go:embed *.sql` and exports `Definition`, `BootstrapDefinitions`,
and `Checksum`. Root's `schema.go` drops its own `//go:embed`, makes
`postgres.Definition` a type alias for `migrations.Definition`, and makes
`postgres.BootstrapDefinitions` a thin forwarder.
`BootstrapDefinitionsWithoutContentSearchIndexes` stays in root (it needs the
content store's deferred DDL) and now sets the exported `Variant` and
`FullChecksum` fields instead of the former unexported `variant` and
`fullChecksum`. `schema_bootstrap_lock.go`'s `migrationVariant` and the
tracker read the same two exported fields, and `migrationChecksum` forwards
to `migrations.Checksum`. No `.sql` file is added, removed, renamed, or
edited.

No-Regression Evidence: a golden test
(`migrations/embed_invariant_test.go`) captured `BootstrapDefinitions()` at
the base commit as a sha256 digest over `Name|Path|sha256(SQL)` per
definition in order (141 definitions, digest
`4167ad0e0bb49fc9cfbcea243c9672b1ceddfc860db7ead5a4b1526a619ed7e4`), then
asserts the identical digest after the split -- Name, Path, SQL, and order
are byte-identical, which matters because the migration tracker keys applied
migrations by `path + variant + checksum_sha256`. A second test confirms the
`*.sql`-only embed pattern excludes `embed.go`, `doc.go`, `README.md`, and
`AGENTS.md`. `go build ./...`, `go vet ./internal/storage/postgres/...`, and
`go test ./internal/storage/postgres/... -count=1 -race` all pass. One test,
`supply_chain_suppression_migration_live_test.go`, read the embedded FS
directly (`embeddedMigrations.ReadFile("migrations/083_...")`); it now calls
`MigrationSQL("supply_chain_suppression_expiry")`, root's existing lookup by
definition name, so it depends on the same `BootstrapDefinitions()` contract
instead of a package-private embed variable.

No-Observability-Change: no metric, span, log key, or status field is added,
removed, or renamed. This is a pure package-boundary move of embed ownership
and field visibility.
