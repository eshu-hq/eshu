# Item 2: worst-case catch-up backlog at small N (Refs #7638)

Status: **measured — drain is ~1 s with zero refusals at N=5.**
No production code changed in this step. Only this file is new.

## Setup

Same built-binary stack as item 1 (base `1664cf2f`, native Neo4j,
two-repo Flux fixture, run host off-repo per
[run-manifest.md](../../../.agents/skills/eshu-performance-rigor/references/run-manifest.md)).
After the item-1 control verdict, three more quiet mutations grew the
pending pile to N=5 with a wide age spread; the consumer was then flipped
on (`ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED=true`, container-inspect
verified) at 14:07:35Z. 60-minute box stated; used ~4 minutes from the
first backlog mutation through the +60 s drain poll.

## What happened (UTC 2026-10-18, coordinator-queried)

| Obligation (gen prefix) | Created | Settled | Age | Outcome |
|---|---|---|---|---|
| `58bc8712` (superseded) | 12:35:17.279 | 14:07:35.085 | 5,538 s | `obsolete` |
| `ea23c123` (superseded) | 12:35:51.404 | 14:07:35.088 | 5,504 s | `obsolete` |
| `973c0276` (superseded) | 14:05:09.730 | 14:07:35.095 | 146 s | `obsolete` |
| `eea45a28` (superseded) | 14:05:58.746 | 14:07:35.103 | 96 s | `obsolete` |
| `65d3c595` (active) | 14:06:47.931 | 14:07:35.183 | 47 s | `completed` (`phase_not_ready woken=0` → `completed woken=1`) |

- Drain wall: all 5 settled within ~0.2 s of the flip second; full
  consumer phases including `deployment_mapping` within ~1.2 s.
  Pending=0 confirmed at the +60 s poll. Consumer phases for the active
  generation: `backward_evidence_committed` 14:07:35.162,
  `deployment_mapping` 14:07:36.148. (Flip stated at second precision, so
  absolute sub-second latency is not claimed — only the ~0.1 s relative
  spread of the settlement timestamps.)
- Claim-age p99: n=5, so p99 ≈ max = **5,538 s (~1.54 h)**. The old claims
  cost nothing extra for their age in this arm — settlement time is flat
  across the 47 s–5,538 s age range (all within the same second).
- N-way refusal line: **zero refusals.** Engine log outcomes for the drain:
  4 `obsolete`, 1 `completed`, 1 `phase_not_ready` (the fence before the
  completion). No `catalog_changed`, no `no_memo_baseline`. The outcome
  distribution shows superseded generations marked obsolete without a
  maintenance pass (no catalog-scan or memo-check outcome appears for them
  in the engine log).

## Consequence for the flip

At small N the catch-up backlog is a non-event in this arm: only the latest
generation per scope pays one targeted pass; everything older is retired as
obsolete within the same second. The item-4 N-way refusal question is therefore about
bulk onboarding where N obligations are simultaneously *active* (or the
catalog churns mid-drain), not about a tall pending pile per se — a pile
of superseded generations drains for free. A larger-N arm with many
simultaneously-owed active generations would be needed to observe refusal
pressure; this arm bounds the shape, not the scale.

## Caveats

1. N=5 on a 2-repo fixture: shape evidence, not scale evidence. The
   full-corpus and multi-active-generation arms stay open (items 2-ext, 5).
2. Ages mix two old control-arm obligations with three fresh backlog
   mutations — disclosed, not hidden; the flat settlement across ages is
   the point.
