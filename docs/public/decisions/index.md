<!-- docs-catalog
title: Decisions
description: How Eshu records architecture decisions today, where those records live, and the format for new decision records.
type: project
audience: contributor, practitioner
entrypoint: false
landing: true
-->

# Decisions

This page is the front door for architecture decisions. It is honest about
where things stand today: Eshu does not keep a separate set of
published ADRs (architecture decision records) on this site yet. Decisions
are recorded as numbered design notes alongside the work they shaped. This
index points you at the real homes and sets the format for new records. No
decision has been invented or back-dated to fill this page.

## Where decisions live today

- [Design notes](https://github.com/eshu-hq/eshu/tree/main/docs/internal/design/)
  — the main home. Files are named with the issue number that drove the
  work, such as
  [7331-neo4j-primary-graph-backend-decision.md](https://github.com/eshu-hq/eshu/blob/main/docs/internal/design/7331-neo4j-primary-graph-backend-decision.md),
  [4574-fact-idl-decision.md](https://github.com/eshu-hq/eshu/blob/main/docs/internal/design/4574-fact-idl-decision.md),
  and
  [4683-incident-routing-sql-decision.md](https://github.com/eshu-hq/eshu/blob/main/docs/internal/design/4683-incident-routing-sql-decision.md).
  These are maintainer design records, not published site pages.
- [Internal architecture reviews](https://github.com/eshu-hq/eshu/tree/main/docs/internal/)
  — periodic direction, performance, and scale reviews sit next to the
  design notes.
- [Architecture](../architecture.md) — the published summary of the system
  as it stands. When a decision changes the shape of Eshu, this page is
  where the result shows up for readers.

## Format for a new decision record

When a choice is expensive to reverse, write it down where the next person
will look. Keep it short and plain:

1. **Context** — what forced the choice, in a few sentences.
2. **Decision** — what we chose, in one clear statement.
3. **Consequences** — what gets easier, what gets harder, and what we
   accepted knowingly.
4. **Alternatives** — what we considered and why we did not pick it.
5. **Status** — proposed, accepted, or superseded, with a link to whatever
   replaced it.

Name the file with the driving issue number, like the existing design
notes, and link it from the pull request that acts on it. If the decision
changes public behavior, update [Architecture](../architecture.md) or the
matching reference page in the same change, and add the decision to the
list below.

## Published decision records

None yet. New records written for readers will be listed here as they land.
Until then, the design notes above are the record.
