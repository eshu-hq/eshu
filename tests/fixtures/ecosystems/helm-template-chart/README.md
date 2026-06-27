# helm-template-chart fixture

Synthetic single Helm chart for the B-7 golden corpus gate's rc-35 correlation
(`HELM_TEMPLATE_VALUE_REFERENCE`).

`templates/deployment.yaml` uses `{{ .Values.<dotted.path> }}` expressions
(`replicaCount`, `image.repository`, `image.tag`, `image.pullPolicy`,
`service.port`, `resources.limits.cpu`, `resources.limits.memory`). Each resolves
to the matching leaf key defined in `values.yaml`, so the parser emits a
`HelmTemplateValueUsage` content entity per usage and a `HelmValueDefinition`
content entity per `values.yaml` leaf. The projector structural-edge phase links
each usage to its definition with a `REFERENCES` edge carrying
`evidence_kinds=["HELM_TEMPLATE_VALUE_REFERENCE"]`.

The gate asserts at least one such edge exists and that the count is provably
zero without this fixture (no other corpus repo defines these node labels).

All data is fabricated; there are no real registries, services, or hosts.
