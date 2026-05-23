# AGENTS.md - services/cloudfront/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are `ListDistributions` and `ListTagsForResource`.
- Wrap each page or point read in `recordAPICall`; keep global-service
  telemetry bounded.
- Do not add `GetDistributionConfig`, mutations, object reads, policy document
  fetches, certificate body reads, private-key handling, or origin custom
  header value persistence.
- Map only scanner-owned metadata and keep identifiers, tags, header values,
  page tokens, raw AWS errors, and secrets out of metric labels.
