# AGENTS.md - services/eventbridge/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are `ListEventBuses`, `ListRules`, `DescribeRule`,
  `ListTargetsByRule`, and `ListTagsForResource`.
- Wrap every page and point read in `recordAPICall`.
- Do not add `PutEvents`, rule/target mutations, event bus policy persistence,
  target payload fields such as `Input`, `InputPath`, `InputTransformer`, or
  `HttpParameters`, credential, STS, graph, or reducer behavior here.
- Keep bus names, rule names, target IDs, ARNs, tags, page tokens, and raw AWS
  errors out of metric labels.
