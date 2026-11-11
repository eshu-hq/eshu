# S7 design-criterion amendment ruling — arbiter (2026-11-11)

RULING: S7 BLOCKED on pre-amendment #10559+#88 — owner 1h order may reset the S6 duration, but does not make 1h evidence equal 72h truth; amend the design criterion in the same PR pair, fix the broken evidence pointer, narrow/re-scope unmet long-horizon claims, then merge the pair together.

Arbiter: `arbiter-eshu`. This ruling answers the P1/P2 review threads on PR #10559.

## 1. 1h vs 72h — owner may shorten, S7 may NOT claim 72h properties from 1h

Verified: the design requires at `docs/internal/design/10037-ghsa-window-scope-decision.md:361` a "72-hour tokened run … lag under `refresh_interval`+1h, no `no_progress`, <2,500/h, no secret", gated by line 353 "until each holds, with evidence in the S7 PR". Prior ruling `docs/internal/arbiter/10037-remainder-ruling-2026-11-09.md:138`: "An owner preference alone lifts none of them." The owner order in #10465 ("Owner order 2026-11-11: the tokened run is 1 hour, not 72 hours") is an explicit acceptance to *replace* that duration, not evidence the 72h soak behaved. The green 1h record (6 claims, 3 ticks x both types, 1382 facts, fresh/succeeded, 0 terminal) proves planning/mint/fetch/commit/cursor-advance only. It expressly disclaims lag/longer-horizon. Therefore: merging on 1h is permitted **only** under an amended criterion (decision 2); leaving the 72h line while the S7 PR declares activatable on the 1-hour run is the P1 overclaim.

The S6 evidence pointer must name the run record (issue #10465 comment `6788316515`), never the unrelated first_epss healing note (`6789735481`).

## 2. Amendment required in the S7 core PR (no silent narrow-only path)

Replace the 72-hour criterion lines with the 1-hour criterion: owner order 2026-11-11 supersedes the original duration; recorded risk states lag stability, request-budget headroom, no_progress, malware volume, and late-record settle are proven only within the 1-hour window, not over 72h; no_progress none, no secret in any surface, cursors advance each tick, with evidence at `https://github.com/eshu-hq/eshu/issues/10465#issuecomment-6788316515`. Malware volume and late records: reported from the 1-hour run only; any 72h-volume/settle claim is NOT_CHECKED and tracked as activation follow-up on #10465, not implied by S7.

Simultaneously narrow all S7 prose to activatable-via-rolling with host-minted token on the 1-hour S6 evidence (owner-accepted shortened soak; see amended activation criteria) — delete any wording implying design-as-written 72h satisfaction. If the coordinator will not amend, S7 stays blocked until a real 72h run closes.

## 3. Pairing — two PRs are the rule, single PR is not required

Design S7 "together" / "S7 is a pair", prior ruling "Execute only as paired PRs". Core #10559 + satellite #88 satisfy it iff: symmetric cross-refs, both green on pushed heads, merged back-to-back in one session (either order; #10465 closed only after both merge), neither merges alone.

## Required proof before merge

Amended design diff + corrected S6 URLs; `mkdocs build --strict` exit 0; `git diff --check` clean; fresh review with P0=0, P1=0, P2-blocking=0 on final heads. Checklist items each marked met with pointer or NOT_CHECKED+follow-up — no silent tick.
