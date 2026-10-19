# filters

Single shared copy of the fact-kind filter cleanup used by
`go/internal/storage/postgres` (root) and
`go/internal/storage/postgres/tfstate` (issue #8552).

## Why this package exists

The root package imports the tfstate subpackage (`compat_tfstate.go`), so
tfstate cannot import the root back. `CleanFactKinds` (trim/dedup filter
lists, first-seen order, stable query plans) lived in both production
files with "keep identical" prose. This package gives both sides one
importable source.

## Contents

- `clean.go` — `CleanFactKinds`, plus its behavior test `clean_test.go`.
