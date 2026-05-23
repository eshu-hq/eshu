# AGENTS.md - services/elbv2/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are ELBv2 describe/list families for load balancers, listeners,
  listener rules, target groups, and tags.
- Wrap every page, batch, and point read in `recordAPICall`.
- Do not add target health reads, access-log reads, policy persistence,
  mutations, credential, STS, graph, or reducer behavior here.
- Keep identifiers, ARNs, tags, host/path match values, page tokens, and raw AWS
  errors out of metric labels.
