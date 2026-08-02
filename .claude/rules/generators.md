---
paths:
  - "scripts/generate-*.sh"
  - "scripts/lib/**"
---

# Generators and generated artifacts

**Load `generator-script-discipline`.**

Never hand-edit a generated artifact. Change the generator, re-run it, and commit
the result — a hand-edit survives exactly until the next regeneration and reads
as drift when it disappears.

Run the regeneration before `make pre-pr`, not after. A generated file committed
afterwards dirties the tree and invalidates the per-SHA stamp, which costs a
second full gate run.
