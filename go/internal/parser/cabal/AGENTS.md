# AGENTS.md - internal/parser/cabal guidance

## Read first

1. `README.md` - purpose, surface, and bounds.
2. `doc.go` - godoc contract.
3. `cabal.go` - the `tested-with` scanner and `Parse`.

## Change guidance

- Read `tested-with` only; any new field needs a credential review first.
- Fail closed on anything the scanner does not literally understand.
