# Evidence: content-entity bucket registration (#5531)

No-Regression Evidence: Restoring five bucket→label pairs to
`snapshotEntityBuckets` (`terraform_blocks`, `cloudformation_conditions`,
`cloudformation_cross_stack_imports`, `cloudformation_cross_stack_exports`,
`protocol_implementations`) adds entries to a static struct-literal slice that
`entityBucketsFromParsed` already walks once per parsed file. It introduces no
Cypher, no query, no goroutine, no lease or claim, no batching knob, and no I/O;
the loop bound grows from 76 to 81 entries.

Measured on the B-7 golden corpus (30 fixture repos, NornicDB + Postgres via
`docs/public/run-locally/docker-compose.yaml`, driven by
`scripts/verify-golden-corpus-gate.sh`) across two clean runs on this branch:
`phase_collect` observed 17.0s against a 20.0s baseline and a 25.0s ceiling —
at or below baseline, not above it. Terminal state was `531 pass,
0 required-fail, 0 advisory-warn`, with the drain reaching zero residual queue
depth and no dead letters. An earlier run on a contended box reported
`phase_collect` 39.0s; that run shared the machine with a concurrent `make
pre-pr`, and the two clean runs are what this note relies on.

Row counts after the change: `content_entities` holds 5 new `TerraformBlock`
rows across the two Terraform corpus fixtures (1 and 4), and NornicDB holds 0
`TerraformBlock` nodes — correct, because the label is deliberately absent from
the projector's `entityTypeLabelMap` and stays content-store-only until #5954
wires the graph layer. The CloudFormation buckets emit nothing in the corpus
today: no fixture carries `Conditions` or cross-stack import/export content, so
their coverage rests on the unit regression test rather than a live run.

No-Observability-Change: the change adds no new pipeline stage, metric, span,
or log site. Emission stays on the existing `content_entity` fact path with its
existing instrumentation, so there is no new operator-facing signal to expose
and no telemetry contract entry to add.


See `README.md` (the Internal flow section) for the three-list sync invariant
itself, and `go/internal/content/shape/bucket_sync_gate_test.go` (CI gate
`content-entity-bucket-sync`) for the gate that enforces it.
