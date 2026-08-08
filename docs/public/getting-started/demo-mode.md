# Demo Mode

`eshu demo` brings up a self-contained Eshu stack seeded with a synthetic
organization, waits until it can actually answer, asks the first question from
the demo manifest, and prints a guided path. No provider credentials are
involved at any point.

```bash
eshu demo up
```

On a warm image cache this reaches a correlated answer in a few minutes. The
answer is real: the corpus is the same one the golden-corpus gate proves,
replayed as the `acme` org, so every demo answer is backed by a fixture CI
already runs.

## What you get

```text
First answer
  Q: Which workload does the api-svc repository run in, and which functions
     handle its routes?
  A: Workload api-svc (kind: service) is defined in repository api-svc.
     API surface exposes 2 …
  Truth: basis=hybrid capability=platform_impact.context_overview
         freshness=fresh level=derived profile=production
```

The truth labels are the point. The demo does not print a confident sentence
and leave you to trust it — it tells you how the answer was derived and how
fresh the evidence is, the same labels every other Eshu read surface carries.

## Commands

| Command | What it does |
| --- | --- |
| `eshu demo up` | Bring the stack up and reach a first answer |
| `eshu demo status` | Report whether the stack is running and indexed |
| `eshu demo down` | Remove the stack, its volumes, and its networks |

Add `--json` to any of them for the `{data, truth, error}` envelope, including
per-phase timings.

## It stays out of your way

The demo runs under its own Compose project (default `eshu-demo`), so it never
adopts or tears down a stack you started for real work. If that project is
already running, `up` says so and stops rather than taking it over. Use
`--project <name>` to run more than one demo side by side.

`eshu demo down` removes containers, volumes, and networks together. A plain
`docker compose down` leaves named volumes behind, which is why the command
does not just wrap it.

## Readiness is completeness, not health

`up` does not return when the containers are running. It waits until the stack
reports itself healthy **and** has indexed at least one repository **and** has
no outstanding queue work. A stack that is merely up answers the demo questions
incompletely, so returning early would hand you a thin answer and call it
success.

## Credentials

The demo is credential-free to you, not to the stack. The MCP server refuses to
start without a resolvable credential source, so `eshu demo up` mints an
ephemeral key for the run, uses it, and discards it with the stack. Nothing is
written to your environment and no key outlives `eshu demo down`.

## When it does not work

`up` reports the phase that failed and carries the underlying Compose output,
so you rarely need to re-run Compose by hand. Two common cases:

- **Docker is not running.** Preflight fails before anything is started and
  names what it probed.
- **Indexing does not settle.** `up` reports the last repository count it saw
  and points at `docker compose -p eshu-demo -f docker-compose.demo.yaml logs`.

## Next

The guided questions `up` prints continue the story past the first answer —
deployment, cloud resources, incidents, and cross-repo dependencies. See
[Your First Five Questions](first-five-questions.md).
