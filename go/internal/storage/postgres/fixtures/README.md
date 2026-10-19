# fixtures

Single shared copy of the drift-evidence test helpers used by both
`go/internal/storage/postgres` (root) and
`go/internal/storage/postgres/tfstate` (issue #8552).

## Why this package exists

The root package imports the tfstate subpackage (`compat_tfstate.go`), so
tfstate cannot import the root back. The parser-shape config fixture
builders and `padQueueFakeRow` lived in both test trees with "keep
identical" prose. This package gives both sides one importable source.
Production filter cleanup lives in the sibling `filters` package.

## Contents

- `fixtures.go` — `ConfigParserRow` / `ConfigResourcesArray`
  (parser `terraform_resources` row shape) and `PadQueueFakeRow`
  (queue-fake width adaptation for newer scan shapes). Test use only.
