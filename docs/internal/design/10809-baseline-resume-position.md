# NVD baseline resume position and seed rotation (#10809, #10810)

Status: **Implemented in core**; the collector half (`eshu-collector-vulnerability-intelligence`
#199) stamps the two new verdict keys. Extends
[10728-baseline-pacing.md](10728-baseline-pacing.md) sections 1 and 3, which
cover year order and the checkpoint. Decisions 4 to 7 of the 2026-11-16 baseline
pace ruling.

## Seed rotation: forward resumes, rollback refuses at commit (#10810)

The old plan-time hold (the checkpoint's seed sha no longer matched the
manifest) is deleted. It could never be evaluated: the coordinator cannot read
the manifest, and the SDK claim carries only the scope. Instead the collector
stamps `nvd_baseline_year_generated_unix_us` (the manifest's `lastModified` for
the year) beside the sha, and the checkpoint writer runs inside the generation's
commit transaction. The guard is part of the same upsert (`DO UPDATE ... WHERE`):
a stored stamp newer than the candidate's, or equal under a different sha,
touches no row, the writer returns an error with failure class
`nvd_baseline_seed_rollback`, and the whole generation rolls back. Folding the
guard into the upsert evaluates it against the locked, latest row version, so a
rollback cannot slip past a newer commit under READ COMMITTED (the live test
holds two transactions open). Forward is safe because a record's `lastModified`
only moves later, so no record moves from ahead of the position to behind it; a
record changed after a chunk read it is the window path's job. A candidate with
no stamp (an older collector, or a manifest entry with no `lastModified`) skips
the guard and commits as before, and it never clears a stored identity: the
position and chunk identity advance, but the stored sha and stamp stay, so a
later candidate stamped with an older seed is still refused. The sha and stamp
move together, only on a stamped write at an equal or greater position.

A refused claim fails retryable under its own class `nvd_baseline_seed_rollback`.
The generation dead letter is recorded under `commit_failure`, and after
`MaxAttempts` the claim goes terminal as `attempt_budget_exhausted`, so a seed
that never catches up ends dead-lettered rather than retrying forever. The
planner re-mints the same position after a refused claim and waits the 30 minute
retry wait. The forward-rotation counter (reported only when the stored seed
identity actually moves, that is the candidate is at or past the stored
position) and the committed-chunk INFO line fire only after the commit lands.

## The `advance_to` contract (`nvd_baseline_advance_to_unix_us`)

The satellite stamps one `nvd_baseline_advance_to_unix_us` per chunk beside
`nvd_baseline_year_sha256`, `nvd_baseline_year_complete`, and
`nvd_baseline_should_advance`, and, when the position falls inside an instant,
`nvd_baseline_advance_to_cve_id`; it also stamps the seed generation
`nvd_baseline_year_generated_unix_us` (the section above). The two keys added by
#10809 and #10810 are optional on the wire so an older collector commits as
before; the shared SDK vector for them lands with the satellite. The host
commit policy (`NVDBaselineCommitPolicy`) decides from them:

- **Meaning.** The position `(advance_to, advance_to CVE ID)` names the
  **first record the chunk did not emit**, in `(lastModified, CVE ID)` order.
  It is not "the last committed record". The next plan starts at it, and the
  start is an inclusive lower bound, so a record is never skipped and never
  doubled. An empty CVE ID means "the first record of that stamp". A chunk may
  cut inside an instant by CVE ID, which is how a same-stamp group larger than
  one chunk drains across chunks.
- **Precision.** The stamp is whole UTC milliseconds, the same form as the
  scope id and the claim's `ModifiedStart`. The host floors a finer value
  (`NormalizeNVDWindowTime`) before it compares or stores it, so a stamp with
  sub-millisecond digits cannot make the stored resume, the planned start, and
  the scope id disagree. The planner floors a stored sub-millisecond stamp and drops its CVE ID,
  because an ID inside a floored sub-millisecond stamp names a position that no
  longer exists; flooring only re-reads, which replays idempotently. A verdict
  that names a CVE ID inside a stamp that is not a whole millisecond is held as
  `position_malformed`, never floored with the ID kept.
- **Progress.** An incomplete verdict (`year_complete` false) must move
  **strictly forward** from the chunk start `(start stamp, from CVE ID)` in
  that row order, comparing CVE IDs as bytes. A verdict that does not is held
  as `not_covered`: the facts commit, the checkpoint stays, and the planner
  re-mints the same scope after the retry wait. A chunker that cannot move
  forward must fail loudly instead of stamping a non-advance. A CVE ID that is
  not canonical, or a seed stamp that is not a non-negative integer, is held as
  `position_malformed` or `generated_malformed`.
- **Completion.** A complete verdict (`year_complete` true) is accepted at any
  position not behind the chunk start. The last chunk of a year can end exactly
  at the stream tail with `complete=false`; the next chunk resumes there, finds
  nothing left, and has no later position to name, so it stamps
  `complete=true` at its own start. That verdict must land `year_complete` or
  the lane stalls on the year. `year_complete` is sticky in the store. No
  verdict, complete or not, may move behind the chunk start.
