# AGENTS.md - services/route53/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are hosted-zone pagination, `ListResourceRecordSets`, and
  `ListTagsForResource`.
- Wrap every page and point read in `recordAPICall`.
- Do not add domain registration, resolver query logs, health-check payloads,
  mutations, credential, STS, graph, or reducer behavior here.
- Keep zone IDs, zone names, record names, record values, tags, page tokens, and
  raw AWS errors out of metric labels.
