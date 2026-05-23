# Webhook Listener Agent Guidance

## Read First

1. `README.md` and `doc.go` for public intake scope.
2. `config.go` for provider routes, auth env, body limit, and branch fallback.
3. `handler.go` and `handler_test.go` for Git provider normalization.
4. `aws_freshness_handler.go` and `aws_freshness_handler_test.go` for AWS
   freshness trigger intake.
5. `handler_observability_test.go` and `main.go` for telemetry and hosted
   runtime behavior.

## Local Rules

- Verify provider authentication and request body limits before normalization.
- Persist trigger decisions only. Do not clone repos, parse payloads into
  facts, connect to the graph, or mark webhook metadata as graph truth.
- Keep provider routes public and admin/metrics routes internal unless an
  operator explicitly protects them.
- Do not store raw webhook bodies without architecture-owner approval, bounded
  retention, redaction rules, tests, and public operator docs.
- Keep repository names, delivery IDs, branch names, SHAs, AWS resource names,
  ARNs, tags, and raw payload fields out of metric labels.
- Keep ignored trigger decisions as terminal decisions; do not turn them into
  refresh work.

## Change Rules

- Add provider config in `config.go`, route behavior in `handler.go`, and
  coverage in `handler_test.go`.
- Add AWS freshness behavior in `aws_freshness_handler.go` with matching tests.
- Add OTEL metric/span/log changes with bounded labels and coverage in
  `handler_observability_test.go`.
- Wire startup in `main.go` only after the handler/store contract is tested.
