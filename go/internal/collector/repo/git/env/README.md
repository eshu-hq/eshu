# env

Environment coercion helpers extracted from the git collector's config
loader (`selection_config.go`) to keep that file under the repo's 500-line
cap. Pure standard-library functions the loader shares across its sections;
a helper with one call site stays with that caller instead of moving here.

## Layout

- `doc.go` — the godoc contract.
- `env.go` — `FirstNonEmpty`, `StringValue`, `BoolFromEnv`.
