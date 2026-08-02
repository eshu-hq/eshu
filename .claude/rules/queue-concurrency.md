---
paths:
  - "go/internal/reducer/**/*.go"
  - "go/internal/projector/**/*.go"
---

# Reducer, projector, and queue behavior

**Load `concurrency-deadlock-rigor`.** Add `eshu-correlation-truth` when the
change decides projected graph truth, and `eshu-postgres-rigor` when it touches
queue SQL.

Serialization is not a fix. Lowering worker count, forcing batch size 1, or
single-threading a drain to make a race disappear hides the defect rather than
removing it — the canon states the full rule and the accepted exceptions.

The failure modes here are coupled more tightly than they look: terminal-status
handling, dead-lettering, non-counting retry classes, and elapsed-since-enqueue
each interact with the others, and a fix aimed at one has repeatedly broken
another. Prove convergence in both directions over the real queue.
