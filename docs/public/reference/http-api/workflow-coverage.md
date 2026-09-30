# CI/CD workflow coverage

Repository-scoped CI/CD run-correlation responses include
`evidence_summary.static_workflow_artifacts`. Repository and service stories
preserve the same static workflow coverage information.

Static workflow evidence describes indexed file metadata. It does not prove a
provider run occurred or that an image was built or deployed.

## Candidate file limit

The static reader examines the first 5,000 indexed repository files in path
order, then identifies GitHub Actions workflows. It keeps that selection order
and limit before filtering by workflow path or artifact type.

When the returned file page reaches 5,000 records,
`candidate_pool_status` is `unknown_at_limit`. Reaching the limit does not
prove that more files exist, and it does not prove that the repository scan is
complete.

| Observed workflow files | Candidate page | State | Coverage |
| --- | --- | --- | --- |
| None | Fewer than 5,000 files | `absent` | No workflow in the indexed file pool read |
| None | 5,000 files | `unknown` | The bounded prefix cannot establish absence |
| One or more | Fewer than 5,000 files | `present` | Workflow evidence observed |
| One or more | 5,000 files | `present` | Observed count and paths; candidate coverage remains uncertain |

The marker is omitted for an uncapped page. Missing repository scope, an
unavailable content store, and a failed read keep their separate
`not_checked` or `unavailable` states.

For example, a repository may have 5,001 indexed files and its only workflow may
sort last. The reader still examines only 5,000 files. Its static summary is
`state=unknown`, `count=0`, and
`candidate_pool_status=unknown_at_limit`; it cannot report repository-wide
absence.

## Returned paths and image evidence

The `count` is the number of workflow files observed in the candidate page.
When candidate coverage is uncertain, it is an observed count rather than a
proven repository total. The returned `paths` stay sorted and limited to 20.
`truncated=true` continues to mean that the displayed path list was shortened;
it is separate from candidate-pool uncertainty.

Image evidence still uses the existing bounded hydration of up to 50 workflow
files. Candidate-pool coverage does not establish complete image-reference
coverage. Static image references remain separate from live run and artifact
evidence.

A capped static scan must not produce the summary reason
`no_ci_cd_evidence_found`. Its uncertainty remains visible in HTTP, MCP, and
story responses, including when live run correlations are unavailable or
empty. Typed CI/CD summaries add `static_workflow_coverage_unknown` to
`missing_evidence` for every capped page, independently of live evidence.
Stories add that coverage class only for capped pages; their preexisting
missing-evidence serialization is preserved for uncapped pages. An empty
capped page has static reason `repository_file_scan_limit_reached`. When live
runs are missing, its summary reason is `static_workflow_coverage_unknown`;
existing live-unavailable and live-present reason precedence is preserved.

See [evidence and supply-chain routes](evidence-and-supply-chain.md) for the
CI/CD list, count, and inventory contracts, and [story routes](story-routes.md)
for repository and service stories.
