# AGENTS.md - internal/coordinator/environment guidance

## Read first

1. `go/internal/coordinator/environment/README.md` for this package's
   ownership boundary and the `env/` gitignore gotcha.
2. `go/internal/coordinator/environment/value.go` for the three parse
   functions.
3. `go/internal/coordinator/config.go` for the root's `LoadConfig` caller.
4. `go/internal/coordinator/semantic/provider_config.go` for the
   `coordinator/semantic` worker-config caller.

## Invariants

- Never rename or move this package to `env/`; the repository's `.gitignore`
  line 49 excludes any `env/` directory from git, so a package there would
  build locally and silently never be committed.
- Keep the three functions free of any coordinator-specific defaults, key
  names, or error-handling policy; those stay with each caller.
- Keep a missing or blank value resolving to `fallback` with a nil error, and
  keep a non-blank unparseable value an error naming the key.

## Common changes

- A new typed parser (for example a string-slice or enum parser) needs a
  failing test for its blank, fallback, and invalid-value cases before the
  implementation, matching the shape of `Int`, `Bool`, and `Duration`.
- Do not add coordinator-specific environment variable names here; add them
  in the caller's own config file and pass the key through.

## Failure modes

- An unparseable non-blank value returns the zero value and an error naming
  the key; callers must not treat the zero value as a usable fallback on
  error.
- `Bool` rejects any string `strconv.ParseBool` rejects, including common
  alternates like `yes`/`no` or `on`/`off`.

## Verification

Run this package's focused tests, then the recursive coordinator tree
(`internal/coordinator/...`) since both `LoadConfig` and the semantic worker
config depend on these functions at startup.
