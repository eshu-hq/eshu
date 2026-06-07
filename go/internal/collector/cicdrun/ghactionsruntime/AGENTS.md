# AGENTS.md — cicdrun/ghactionsruntime guidance

## Read First

1. `README.md` — package purpose, provider contract, and safety rules.
2. `source.go` — claim-to-fact flow and runtime target validation.
3. `client.go` — GitHub REST pagination and request bounding.
4. `../AGENTS.md` — fixture normalizer boundary. Do not move live HTTP code
   into the parent package.

## Invariants

- Keep GitHub Actions provider polling in this runtime package, not in
  `internal/collector/cicdrun`.
- Fetch only configured repositories and bounded workflow-run, job, and artifact
  pages.
- Strip query strings and fragments from artifact download URLs before facts are
  emitted.
- Preserve provider-native run IDs, run attempts, job IDs, and artifact IDs.
- Emit warnings for partial job or artifact metadata instead of publishing
  complete-looking facts.
- Do not infer deployment truth from workflow success, job names, artifact names,
  environment names, tags, or repository names.
