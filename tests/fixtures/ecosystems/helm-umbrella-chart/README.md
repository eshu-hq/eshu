# helm-umbrella-chart

B-7 corpus fixture. A Helm umbrella chart whose `Chart.yaml` declares a
dependency on the in-corpus `deployable-source` repository via the subchart
`repository:` URL `https://github.com/acme/deployable-source`. The relationships
engine emits `HELM_CHART_REFERENCE` evidence resolving to `deployable-source`,
materialising a `(:Repository)-[:DEPLOYS_FROM]->(:Repository)` edge.

The golden-corpus gate asserts this edge filtered on
`evidence_kinds=[HELM_CHART_REFERENCE]`, isolating the Helm chart-dependency
deployment-source verb from the Kustomize (rc-29) and ArgoCD (rc-19)
`DEPLOYS_FROM` edges.

No proprietary data: all identifiers are synthetic (`acme` org).
