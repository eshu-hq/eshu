# Canonical Terraform package guidance

## Ownership

- This package owns Terraform state row shaping, Cypher templates, resolver
  contracts, and statement-free parts.
- The parent `cypher` package owns `Statement` wrapping, the writer API, and
  placement of the Terraform phase in canonical dispatch.

## Invariants

- Keep Cypher templates byte-identical unless the task explicitly changes graph
  behavior and supplies the required backend and performance evidence.
- Keep resolver ambiguity fail-closed and distinguish authoritative ownership
  outcomes from transient failures.
- Keep `Drain` set on MATCHES_STATE relationship retractions.
- Do not import the parent `cypher` package.
