<!-- capability-state: id=security.hardcoded_secrets state=general_availability issue=3018 -->
# Hardcoded Secrets Investigation

`investigate_hardcoded_secrets` is a read-only investigation over Eshu's
indexed content store. It scans the lines Eshu has already ingested for
password literals, API keys, tokens, private keys, and other risky literals,
then returns **redacted** findings with risk classification, suppression
metadata, paging, and coverage.

The tool never returns a raw secret value. Every candidate line is passed
through redaction before it leaves the handler, so the response carries enough
context to locate and triage a finding without re-exposing the secret itself.

The capability id is `security.hardcoded_secrets`. It is marked
`general_availability` in the capability catalog and resolves from the bounded
content index across all supported query profiles.

## What is detected

Each finding is classified into exactly one `finding_kind`. The classification
is derived from the matched line text, in priority order:

| `finding_kind`     | Meaning                                                          |
| ------------------ | --------------------------------------------------------------- |
| `aws_access_key`   | An AWS access key id literal (the `AKIA…` shape).                |
| `private_key`      | A PEM private-key block header (`-----BEGIN … PRIVATE KEY-----`).|
| `slack_token`      | A Slack-style token literal (the `xox…` shape).                  |
| `api_token`        | An `api_key` / `apikey` / `token` assignment literal.           |
| `password_literal` | A `password` / `passwd` / `pwd` assignment literal.             |
| `secret_literal`   | A `secret` / `client_secret` / `private_key` / `authorization` assignment literal. |

Detection is pattern-based over indexed file content. A line that does not match
any known shape is dropped (it is not returned with an empty kind).

## What is redacted, what is returned

The response carries **metadata only**. There is no field that contains the raw
secret value.

Each finding object contains:

- `rank` — 1-based position of the finding within the returned page.
- `repo_id` — canonical repository identifier the line belongs to.
- `relative_path` — repository-relative file path.
- `language` — indexed language for the file (may be empty).
- `line_number` — 1-based line within the file.
- `finding_kind` — the classification described above.
- `confidence` — `high`, `medium`, or `low`.
- `severity` — `critical`, `high`, or `medium`.
- `redacted_excerpt` — the matched line with the secret value replaced by a
  `[REDACTED]` marker.
- `suppressed` — boolean; true when the finding matched a suppression rule.
- `suppression_notes` — the list of suppression reasons (see below); empty when
  not suppressed.
- `source_handle` — a citation handle: `repo_id`, `relative_path`, `start_line`,
  `end_line`. The handle points at the line so it can be fed to a downstream
  evidence-citation call.

### Redaction

Redaction happens in the handler before serialization. The matched line is
rewritten so that:

- An assignment literal (for example `token = …`) keeps the key and the
  assignment operator, and replaces the value with `[REDACTED]`.
- A standalone secret shape (AWS access key, `sk_live_…`, Slack token, PEM
  private-key header) is replaced entirely with `[REDACTED]`.

The raw line text is scanned and classified internally, but only the redacted
form (`redacted_excerpt`) is emitted. Coverage reports this explicitly with
`redaction: "secret_values_replaced_with_redacted_marker"`.

### Confidence and severity

Confidence and severity are derived from the `finding_kind`:

| `finding_kind`     | `confidence` | `severity` |
| ------------------ | ------------ | ---------- |
| `aws_access_key`   | `high`       | `critical` |
| `private_key`      | `high`       | `critical` |
| `slack_token`      | `high`       | `critical` |
| `api_token`        | `medium`     | `high`     |
| `password_literal` | `medium`     | `high`     |
| `secret_literal`   | `medium`     | `high`     |

Provider-style keys with a distinctive shape carry higher confidence than
generic assignment literals, because the shape itself is strong evidence.

## Suppression notes

A finding is suppressed when it is more likely to be test, fixture, example, or
placeholder material than a live secret. Suppression is computed from the file
path and the line text, and each match adds a reason to `suppression_notes`:

- `test_or_fixture_path` — the path contains one of `_test.`, `/testdata/`,
  `/fixtures/`, or `/examples/`.
- `placeholder_literal` — the line contains one of `example`, `dummy`,
  `placeholder`, or `changeme`.

By default (`include_suppressed = false`), suppressed findings are excluded from
the response entirely. Set `include_suppressed = true` to surface them; they are
returned with `suppressed: true` and their `suppression_notes` populated so a
reviewer can see *why* Eshu down-weighted them.

The `coverage` block reports `suppressed_count` and the `include_suppressed`
flag. `suppressed_count` counts only the suppressed findings *among the rows the
query returned*. Because the default query (`include_suppressed = false`)
filters suppressed rows out before they are counted, `suppressed_count` is `0`
in that mode — including in the zero-finding case. It does not reveal whether
suppressed candidates existed. To find out whether candidates were down-weighted
as fixture or placeholder noise, re-run with `include_suppressed = true`: then
the suppressed rows are returned and `suppressed_count` reflects them.

## Tool and HTTP

**Tool name:** `investigate_hardcoded_secrets`

**Description:**

> Investigate potential hardcoded passwords, API keys, tokens, private keys, and
> risky literals from indexed content with redacted findings, suppression
> metadata, paging, and coverage.

**Input schema:**

| Field                | Type      | Default | Notes                                                   |
| -------------------- | --------- | ------- | ------------------------------------------------------- |
| `repo_id`            | string    | —       | Optional. Scope the investigation to one repository.    |
| `language`           | string    | —       | Optional. Filter by indexed language.                   |
| `finding_kinds`      | string[]  | —       | Optional. Each value must be one of `api_token`, `aws_access_key`, `password_literal`, `private_key`, `secret_literal`, `slack_token`. An empty list searches all kinds. |
| `include_suppressed` | boolean   | `false` | Include test/fixture/example/placeholder findings.      |
| `limit`              | integer   | `25`    | Maximum redacted findings to return. Capped at `200`.   |
| `offset`             | integer   | `0`     | Zero-based result offset. Must be `>= 0` and `<= 10000`.  |

An unrecognized `finding_kind` is rejected. A non-positive `limit` falls back to
the default of `25`; a `limit` above `200` is clamped to `200`.

**HTTP route:** `POST /api/v0/code/security/secrets/investigate`

### Paging and truncation

The handler fetches one more row than the requested `limit` to detect more
results. If more than `limit` candidates are available, the page is trimmed to
`limit` and `truncated: true` is set at the top level and inside `coverage`.
Advance through pages by increasing `offset`.

The top-level response carries `count`, `limit`, `offset`, `truncated`,
`finding_kinds`, `scope`, `source_backend`, `recommended_next_calls`, and a
`coverage` block. `coverage` includes `query_shape`, `returned_count`,
`suppressed_count`, `include_suppressed`, `limit`, `offset`, `truncated`,
`redaction`, `empty`, `searched_all_kinds`, and `requires_repo_scope`.

## Example invocation and example response

The values below are illustrative and anonymized. Secret values are shown as
`[REDACTED]` exactly as the real response would render them — never use a real
secret in a request or expect one in a response.

Example request:

```json
{
  "repo_id": "repo-example",
  "finding_kinds": ["api_token", "aws_access_key"],
  "include_suppressed": false,
  "limit": 25,
  "offset": 0
}
```

Example response (abbreviated):

```json
{
  "scope": { "repo_id": "repo-example", "language": "" },
  "finding_kinds": ["api_token", "aws_access_key"],
  "findings": [
    {
      "rank": 1,
      "repo_id": "repo-example",
      "relative_path": "src/config/client.go",
      "language": "go",
      "line_number": 42,
      "finding_kind": "api_token",
      "confidence": "medium",
      "severity": "high",
      "redacted_excerpt": "    apiKey = \"[REDACTED]\"",
      "suppressed": false,
      "suppression_notes": [],
      "source_handle": {
        "repo_id": "repo-example",
        "relative_path": "src/config/client.go",
        "start_line": 42,
        "end_line": 42
      }
    }
  ],
  "recommended_next_calls": [
    {
      "tool": "build_evidence_citation_packet",
      "reason": "hydrate exact redacted source context for selected findings",
      "args": { "handles": [ { "kind": "file", "repo_id": "repo-example", "relative_path": "src/config/client.go", "start_line": 42, "end_line": 42, "evidence_family": "security", "reason": "hardcoded secret candidate" } ] }
    }
  ],
  "count": 1,
  "limit": 25,
  "offset": 0,
  "truncated": false,
  "source_backend": "postgres_content_store",
  "coverage": {
    "query_shape": "content_secret_investigation",
    "returned_count": 1,
    "suppressed_count": 0,
    "include_suppressed": false,
    "limit": 25,
    "offset": 0,
    "truncated": false,
    "redaction": "secret_values_replaced_with_redacted_marker",
    "empty": false,
    "searched_all_kinds": false,
    "requires_repo_scope": false
  }
}
```

When no candidates are found, `findings` is empty and `recommended_next_calls`
suggests broadening the investigation with `investigate_code_topic`.

## Comparison framing

A raw regex secret scanner (the GitGuardian / TruffleHog / Snyk Code class of
tool) typically emits every line that matches a secret pattern and leaves the
caller to filter test, fixture, and placeholder noise. Eshu's approach differs
in two concrete ways that are visible in the response:

- **Redaction is part of the contract.** The matched secret value never leaves
  the handler. The response carries a `redacted_excerpt` plus a citation handle,
  not the live value, so the finding can be triaged without re-exposing the
  secret.
- **Suppression evidence travels with the finding.** Eshu classifies fixture,
  test, example, and placeholder material with named reasons
  (`test_or_fixture_path`, `placeholder_literal`) and excludes it by default.
  When `include_suppressed` is set, the reasons are returned alongside the
  finding rather than silently dropped, and `coverage.suppressed_count` reports
  how many candidates were down-weighted.

This is framed as Eshu's posture, not a benchmark claim against any specific
product. The detection surface here is pattern-based over indexed content; it is
the suppression metadata and redaction contract — not a larger pattern set —
that distinguish the response.

## Related

- [Security Intelligence](security-intelligence.md)
- [MCP Reference](mcp-reference.md)
- [Supply Chain Traceability](../supply-chain-traceability.md)
