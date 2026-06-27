# kustomize-deployable-overlay

B-7 corpus fixture. A Kustomize production overlay whose base is the
`deployable-source` repository, declared as a remote Kustomize resource:

```yaml
resources:
  - github.com/acme/deployable-source//k8s?ref=v1.4.0
```

This produces a `KUSTOMIZE_RESOURCE_REFERENCE` evidence fact that resolves to the
in-corpus `deployable-source` repository, materialising a
`(:Repository)-[:DEPLOYS_FROM]->(:Repository)` edge. The golden-corpus gate
asserts it as an evidence-filtered required correlation so the Kustomize
deployment-source verb is isolated from the ArgoCD-emitted `DEPLOYS_FROM` edges
(see the snapshot's `evidence_kinds` predicate).

No proprietary data: all identifiers are synthetic (`acme` org, generic
`deployable-app`).
