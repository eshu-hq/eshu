# NVD baseline implementation lanes (#10728)

Merge order from the ruling: S1, then C1, then K1; C2 after C1; #10746 after C2.

- S1, eshu-sdk: the verdict-key vector with the position cases (ruling section
  (c)).
- C1, core (eshu #10809, #10810, #10732): scope grammar, planner and checkpoint
  with the resume position and the seed-rollback refusal, migration 301.
  Shipped for #10809 and #10810.
- K1, collector #199: the record-atomic chunker, paged chunk, chunk budget
  config, verdict stamps.
- C2, core pace (#10814): the latch streak (migration 302), then the planner's
  drain gate, stale hold, adaptive pace, and telemetry. Shipped.
- #10746: the enable lane over the above, without a seed-manifest input.
- #10736 slice A: `derive_nvd_baseline` is accepted by exact key, loaded by
  `NVDBaselineDerivationFromConfig`. Revert guard: a core release without the
  exemption fails config load, so remove the section, not just `enabled`.
- Proof gates per the ruling: disposable-DB run, R6-style idempotency, the "how
  much of NVD held" count query inside a 15s statement timeout, and the QA
  canary (eshu#10817).
