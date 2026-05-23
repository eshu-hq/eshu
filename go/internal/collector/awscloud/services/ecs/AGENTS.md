# AGENTS.md - services/ecs

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep ECS AWS access behind `Client`; the scanner package must not import the
  AWS SDK.
- Redact task-definition environment values with the configured redactor before
  any envelope is built.
- Preserve secret `value_from` references as references only; never read secret
  values.
- Emit reported resource, relationship, and image-reference evidence only.
- Do not infer workload, environment, repository, ownership, or deployable-unit
  truth from cluster names, service names, tags, task families, or images.
- Keep cluster names, service names, task ARNs, tags, image URIs, environment
  values, raw AWS errors, and page tokens out of metric labels.
