# Evidence: EPSS score-date digest exclusion (#11598)

`contentDigestOf` drops `selected_epss_score_date` from the hashed
provenance; score, probability, and percentile stay covered. Date-only
refreshes now digest-equal (no capture, no fanout); score changes still
flip the digest.

- No-Regression Evidence (#11598): baseline digest input is the full
  typed payload including the ~10-byte date string; after is the same
  input minus one key (one extra map-key comparison per row). Live QA
  keyed-tick decomposition 2026-11-27 (backlog regime, ~59k queued
  keys, ~92-100% duty): apply phase 0.75s mean (16ms/key), dominated
  by build_active_facts at 22.2s mean (0.48s/key). Digest hashing sits
  inside the
  apply-side fraction, so removing one small hashed field cannot move
  tick cost beyond noise. Behavior pinned red-first by the new digest
  tests (fail pre-fix, pass post-fix).
- No-Observability-Change (#11598): no new signals. The effect is
  visible in existing capture/fanout counters and dirty-version growth,
  quantified by the post-deploy M4 measurement tracked on #11598.
