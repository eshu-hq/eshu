# Current-Path Semantic Eval Runner

This package executes semantic eval cases through Eshu's existing HTTP API and
converts the ranked responses into `semanticeval.Run` values. It is the Phase 0
baseline harness for ADR `2026-05-15-nornicdb-semantic-retrieval-evaluation`.

The runner only calls bounded public query surfaces:

- `code_search` -> `POST /api/v0/code/search`
- `code_topic` -> `POST /api/v0/code/topics/investigate`
- `content_file_search` -> `POST /api/v0/content/files/search`
- `content_entity_search` -> `POST /api/v0/content/entities/search`

Cases default to `limit: 10` and reject limits above 50. Each request asks for
the Eshu response envelope so the scorer can distinguish exact graph truth from
derived or fallback content truth. Unsupported capabilities become a single
`unsupported://<case_id>` candidate with `truth: "unsupported"`.

To add a case, include `current_path` beside the normal `semanticeval` case:

```json
{
  "id": "score-path",
  "question": "where does semantic scoring happen?",
  "scope": {"repo_id": "eshu"},
  "expected": [
    {"handle": "entity://score", "relevance": 3, "required": true, "max_truth": "exact"}
  ],
  "current_path": {"mode": "code_search", "query": "Score", "limit": 10}
}
```

Candidate handles are normalized in this order: `source_handle`, `handle`,
`entity_id`, then `file://<repo_id>/<path>` from file path fields.

## Checked-In Starter Suite

`testdata/eshu_phase0_suite.json` contains a 10-case public Eshu starter corpus.
It uses `{repo_id}` placeholders so local canonical repository ids are not
committed. Run it through `go/cmd/semantic-eval-currentpath` with `--repo-id`
set to the indexed Eshu repository id; the command rejects placeholder suites
when `--repo-id` is omitted.

The starter suite is intentionally smaller than the ADR's final 50-100 case
target. Expand it with additional public, non-private operator questions before
using the baseline for product adoption decisions.
