# Webhook Package Guidance

Read these files first:

1. `doc.go`
2. `types.go`
3. `signature.go`
4. `normalizer.go`
5. `normalizer_test.go`

## Invariants

- Keep provider verification separate from payload normalization.
- Do not enqueue refresh work from this package. Durable storage and queue
  handoff belong to the webhook runtime or coordinator boundary.
- Do not treat webhook payload data as graph truth. It is only trigger and
  provenance input for the normal collection pipeline.
- Preserve explicit ignored decisions for non-default branches, tags, and
  unmerged pull or merge requests.
- Keep repository names, delivery IDs, and SHAs out of metric labels.

## Common Changes

- Add provider payload fields in `normalizer.go` when the runtime needs more
  provenance.
- Add new ignored reasons in `types.go` when operators need to distinguish a
  valid but non-refreshing event.
- Add table tests in `normalizer_test.go` before changing provider event
  handling.

## Do Not

- Do not accept GitHub SHA-1 webhook signatures.
- Do not infer default-branch truth from pull request branch names when the
  repository payload or configured fallback is missing.
- Do not introduce provider SDK dependencies for payloads that can be parsed
  with the standard library.
