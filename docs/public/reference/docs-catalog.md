<!-- docs-catalog
title: Docs Catalog Metadata
description: Defines the metadata schema and verifier contract for the human-first documentation IA.
type: reference
audience: maintainer
entrypoint: false
landing: false
-->

# Docs Catalog Metadata

The docs catalog metadata block is the small contract that keeps the GetEshu
human-first documentation IA from drifting. It is intentionally lighter than a
generated site catalog: the first ratchet covers landing and entrypoint pages,
then later PRs can expand coverage when a page family is ready.

Each covered Markdown page carries a hidden block near the top of the file:

```markdown
<!-- docs-catalog
title: Ask Code Questions
description: Gives CLI and MCP examples for asking code, dependency, and call-graph questions.
type: how-to
audience: practitioner
entrypoint: true
landing: false
-->
```

## Fields

| Field | Required | Values |
| --- | --- | --- |
| `title` | yes | Human-readable page title. |
| `description` | yes | One-sentence page purpose for catalog and search use. |
| `type` | yes | `tutorial`, `how-to`, `concept`, `reference`, `operate`, `proof`, or `project`. |
| `audience` | yes | Reader role or comma-separated roles such as `new-user`, `practitioner`, `operator`, or `maintainer`. |
| `entrypoint` | yes | `true` when a reader should be able to reach the page from a landing page. |
| `landing` | no | `true` when the page is allowed to satisfy entrypoint reachability. |
| `time` | no | Expected completion time for tutorials or workflows. |
| `difficulty` | no | Optional complexity hint for future catalog expansion. |

## Initial Subset

The first required subset is the human-first IA established by the #4593 docs
restructure:

- docs home and start-here pages
- first successful run and MCP connection entrypoints
- tutorial landing plus the initial tutorial set
- how-to, concept, reference, and operate landing pages
- first task, contract, proof, and operations entrypoints

The verifier does not require complete metadata coverage for every public doc.
When a page has a `docs-catalog` block, the verifier validates the block. When a
page is in the required subset, the verifier also requires that the file exists
and carries metadata.

## Reachability

Pages marked `entrypoint: true` must be reachable from at least one page marked
`landing: true`. Reachability is based on links present in Markdown or HTML in
the landing page. This keeps the catalog honest: a page is not an entrypoint if
readers cannot get to it from the human-first IA.

## Check

Run the catalog verifier before changing the human docs IA:

```bash
bash scripts/verify-docs-catalog.sh
```

The verifier fails on:

- missing required pages
- missing required metadata blocks
- missing required metadata fields
- invalid `type` values
- invalid `entrypoint` or `landing` booleans
- entrypoint pages that are not reachable from a catalog landing page
