# naming-glue-gate agent guidance

- Never add a maintained word list (a dictionary of accepted proper nouns, a
  denylist of known-bad prefixes) as the detection mechanism. That was the
  explicit reason this gate calls a model instead of a heuristic: a fixed
  list drifts unboundedly as the repo adds domains (AWS/GCP/Azure service
  names alone already number in the hundreds). The only names allowed to
  live in this package are the small, explicitly-cited precedent examples in
  `prompt.go`'s `systemPromptText`, sourced from
  `docs/internal/design/naming-remediation.md` for few-shot grounding, not as
  a lookup table this code checks membership against.
- Keep the candidate filter in `isGlueCandidateShape` a *shape* filter
  (lowercase, unseparated, length floor), never a word-based one. If you find
  yourself adding a specific word or prefix to skip or catch, that word
  belongs in the prompt's precedent list instead, or nowhere.
- `-blocking` defaults to false. Do not flip that default without the
  owner's sign-off — pre-commit wiring passes `-blocking=true` explicitly;
  CI wiring must stay non-blocking until real PR data shows an acceptable
  false-positive rate. See `doc.go`'s "Blocking behavior" section for why.
- Missing `DEEPSEEK_API_KEY` and any classify error MUST fail open (exit 0).
  Do not "fix" this to fail closed even under pressure to make the gate
  stricter — a network hiccup or an unset key in a fresh clone is not a
  naming violation, and this gate must never be the reason an unrelated
  commit or CI job is blocked.
- `Classifier` and `GitRunner` exist so tests never make a real network call
  or touch a real git tree. Every new code path needs a fake-backed test
  before a live one; do not add a test that calls the real DeepSeek API to
  the `go test` suite — that belongs in a manual/local validation script
  only, run by hand with a real `DEEPSEEK_API_KEY`, never in CI.
- The model's response is treated as untrusted structured output, not code:
  parse it strictly into `Report`/`Finding` and never execute or interpolate
  its `evidence`/`suggested_split` text into a shell command or file path.
- This package classifies directory names only (see `doc.go` "Scope"). If
  file-stem glue coverage is ever added, it needs its own explicit ask —
  don't silently widen `candidates.go` to also emit file candidates.
