---
paths:
  - "go/internal/storage/postgres/**/*.go"
  - "go/internal/storage/postgres/migrations/*.sql"
---

# Postgres, schema, and queue state

**Load `eshu-postgres-rigor`.** Add `concurrency-deadlock-rigor` when the change
touches claim, lease, retry, or queue-ordering behavior.

A migration is a one-way door in a way ordinary code is not: it runs once against
data that already exists. Prove the statement against representative data before
writing it, not after.
