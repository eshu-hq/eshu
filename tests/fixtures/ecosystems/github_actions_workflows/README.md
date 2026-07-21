# github_actions_workflows

Golden-corpus fixture for the GitHub Actions workflow-relationship detector
(#5337, #5378). `.github/workflows/ci.yml` carries both discriminating shapes in
one file:

- a genuine step-level `uses: hashicorp/setup-terraform@v3` that MUST produce a
  `DEPENDS_ON` / `github_actions_action_repository` content relationship to the
  Repository `hashicorp/setup-terraform`, and
- a `run: |` block scalar whose heredoc text contains literal `uses:` lines
  (`octocat/example-action@v1`) that MUST NOT produce any relationship — the
  structured-YAML decode treats the block as an opaque string.

`actions/checkout@v4` is present but is excluded from `DEPENDS_ON` action edges
by design.
