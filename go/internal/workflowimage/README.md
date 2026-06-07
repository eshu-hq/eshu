# internal/workflowimage

`workflowimage` extracts public-safe container image evidence from repository
workflow definitions. It is a leaf helper used by collector and query code so
static GitHub Actions workflow command parsing stays consistent across durable
facts and readback summaries.

The package classifies explicit Docker build, buildx, tag, and push commands
into exact, unresolved, or ambiguous evidence. Exact evidence carries a
normalized image reference and command metadata such as workflow path, job,
step, and command kind. It never returns raw shell commands.

Collectors decide how to emit facts from these rows. Reducers decide whether
the evidence can contribute to canonical image identity, and only after registry
facts prove digest or tag identity.
