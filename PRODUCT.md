# Product

## Register

product

## Users

Eshu Console serves a broad internal audience, not only engineers. Daily users
include software engineers, platform engineers, SREs, support teams, directors,
executives, and finance-adjacent stakeholders who need to understand how code,
repositories, deployment systems, workloads, runtime health, and findings relate
to one another.

Users arrive with different depths of technical context. Engineers may need
precise evidence and drilldowns. Support may need fast service and ownership
context. Directors and executives need a readable story about system state,
risk, coverage, and progress. Finance-adjacent users need plain-language
confidence about what exists, what changed, and where operational effort is
going.

## Product Purpose

Eshu Console turns Eshu's code-to-cloud graph into a readable operating surface.
It should show real data, not mockups, and help people move from broad health
and coverage to specific evidence without losing trust.

Success means a user can answer these questions without needing a senior
engineer to translate the graph:

- What does this service or repository do?
- What deploys it, where does it run, and what depends on it?
- What evidence supports that relationship?
- Is indexing healthy, stale, partial, or blocked?
- Which findings need action, and how can I drill into them?
- What is known, what is missing, and what is only inferred?

## Brand Personality

The console should feel vibrant, clear, trustworthy, and alive. It must not feel
dark by default, dull, generic, or like a developer-only database viewer. It
should have enough energy to make a large graph feel approachable, while staying
precise enough that operators trust the data during real work.

The voice is direct and human. It explains graph truth in plain language first,
then exposes the technical evidence behind it for users who need detail.

## Anti-references

Avoid:

- Generic SaaS dashboard patterns with oversized metric cards and decorative
  gradients.
- Dark-card sameness where every panel looks equally important.
- Unreadable relationship graphs with clipped names or unlabeled edges.
- Key/value evidence dumps that require users to reverse-engineer the story.
- Mock data or demo phrasing on real-data surfaces.
- Engineer-only terminology without plain-language context.
- Decorative UI that makes status, risk, freshness, or evidence harder to read.

## Design Principles

1. Show the story, then the proof.
   Every important claim should have a readable summary and a path to the
   underlying evidence.

2. Make graph truth legible to the whole org.
   Use labels, grouping, hierarchy, and drilldowns so engineers, support, execs,
   and finance-adjacent users can all orient themselves.

3. Separate known, missing, inferred, and stale.
   Do not flatten truth levels into one generic status. Missing data and partial
   evidence should be visible and understandable.

4. Use vibrancy with purpose.
   Color should help distinguish relationships, states, risk, freshness, and
   selected evidence. It should not become decoration or visual noise.

5. Optimize for real operational workflows.
   Search, drilldown, filtering, graph exploration, findings review, and runtime
   health need to be efficient on real corpora, not just attractive screenshots.

## Accessibility & Inclusion

The console should target WCAG AA as the baseline. Color must not be the only
indicator of relationship type, risk, freshness, or status. Graphs need readable
labels, keyboard-reachable drilldowns, and non-graph summaries for users who
need table or narrative views.

The interface should avoid motion that distracts from analysis. Any animation
should clarify state changes, selection, loading, or drilldown transitions and
respect reduced-motion preferences.
