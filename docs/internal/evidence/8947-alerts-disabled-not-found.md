# #8947 Disabled-Dependabot 403 Classification Evidence

No-Regression Evidence: the change moves one failure from the `auth_denied`
class to `not_found` inside `GitHubDependabotError.FailureClass`. Both classes
are terminal (`TerminalFailure` lists both), so no claim moves between retry and
terminal, and no request is added, removed, or reordered. The org fan-out reads
the marker first, only for `not_found`, so a repository that answered "alerts
disabled" still continues and one that did not still stops.

## Baseline and after

- Baseline: `origin/main` at 19785f70bb. A 403 with no rate-limit signal and a
  complete JSON body saying Dependabot alerts are disabled classifies as
  `auth_denied` on a repository-scope target.
- After: the same response classifies as terminal `not_found`.
  `TestGitHubDependabotClientAlertsDisabled403IsTerminalNotFound` and
  `TestClaimedSourceRepositoryTargetClassifiesAlertsDisabledAsNotFound` fail on
  the baseline (`class = "auth_denied"`) and pass after.
- Backend/version: none. No Cypher, SQL, queue, lease, or claim code changed.
- Input shape: the 17-case table in
  `TestGitHubDependabotClientMarksAlertsDisabled403` (rate-limit signals win,
  401, 404, 422, 500, HTML, empty, cut-off, reworded, and exactly-at-cap bodies
  keep their class) and the org fan-out cassette scenario
  `alerts_disabled_403_continues_fanout`.
- Terminal counts: unchanged. The fan-out still counts the repository as
  `alerts_disabled` toward the 100-repository cap.

Observability Evidence: `eshu_dp_security_alert_provider_requests_total` and
the fetch-duration histogram follow `FailureClass`, so a disabled repository
now records `status_class=not_found` instead of `auth_denied`; an alert rule on
`auth_denied` for security-alert instances now means a credential or permission
problem only. Readiness shows `terminal fetch failure: not_found
(list_repo_alerts)`, and the runbook and terminal-failure reference say that
means the repository is missing or has Dependabot alerts disabled. The 403 body
shape rests on third-party tools, not a GitHub capture: if GitHub rewords it the
case degrades to `auth_denied` (terminal, fan-out stops), never to a wrong
continue or a retry loop.
