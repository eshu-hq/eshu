# #5743 — EC2 instance identity intent trigger fix (golden-corpus residual)

Trigger `buildEC2InstanceIdentityMaterializationReducerIntent` on the
`ec2_instance_posture` fact — the same fact `DomainEC2InstanceNodeMaterialization`
triggers on — instead of any `aws_resource` fact, so it enqueues only when its
claim-readiness gate (which waits on the EC2 instance node) can be satisfied.

No-Regression Evidence: this is a projector reducer-intent trigger change, not a
performance rewrite; it strictly reduces enqueue volume (a no-EC2-instance aws
scope no longer gets an `ec2_instance_identity_materialization` intent at all).
Baseline (origin/main b002541ec): the B-7 golden corpus gate is red —
`[FAIL] fact_work_items_residual: residual=3` after a 10-minute drain, because
three ecr/lambda/ecs-scope identity intents sat `pending` behind a readiness gate
that could never open. After (this branch, commit content unchanged): the same
gate reports `[PASS] fact_work_items_residual: residual=0`, `493 pass, 0
required-fail, 0 advisory-warn`, `PASS: B-7 golden corpus gate green` in 99s
(ceiling 1800s), on NornicDB `nornicdb-cpu-bge:v1.1.11` over the 20-repo golden
corpus, drain terminal `residual=0`. Backend/version: NornicDB v1.1.11 +
Postgres 16 via the golden-corpus-gate compose stack. Input shape: the fixed
20-repo golden corpus with the aws ec2/ecs/ecr/lambda cassette scopes. Terminal
counts: `fact_work_items` residual 3 → 0; per-phase timings
first_drain=66s, maintenance_drains=9s. Why safe: the change makes the identity
domain enqueue exactly when the EC2 instance node it augments does, matching the
builder's own documented intent (it augments the posture-owned node); the ami_id
write still sources from the co-present `aws_ec2_instance` aws_resource fact the
handler loads, and retract behavior is now consistent with the node domain it
shadows. Fail-before/pass-after unit proof:
`TestBuildProjectionDoesNotQueueEC2InstanceIdentityWithoutPosture`, plus the
lambda fan-out dropping 6 → 5 intents.

No-Observability-Change: the intent builder emits no metric, span, or log of its
own; intent volume stays covered by `eshu_dp_reducer_intents_enqueued_total` and
`eshu_dp_projector_run_duration_seconds`, and the reducer execution that consumes
the intent by `eshu_dp_reducer_executions_total` /
`eshu_dp_reducer_run_duration_seconds`. The telemetry-coverage row for
`ec2_instance_identity_materialization_intents.go` is updated to the posture
trigger in the same change. No metric is added or removed; the enqueue-count
signal simply no longer fires for no-EC2-instance scopes, which was the anomaly.
