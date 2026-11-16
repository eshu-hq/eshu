# Content relationships leaf

## Purpose

`fallback` (entity/content/fallback) holds the entity content-fallback relationship build
(#9669, lane C2): the per-language and per-manifest-kind extraction the
entity context fallback and the code relationships route serve when the
graph has no edge. `Builder` is the production
`querycontract.ContentRelationshipBuilder`; `cmd/api` wires it into the
entity and code handlers directly, and the query root's
`ContentIndexRelationshipBuilder` forwards to it.

## Ownership boundary

This package owns the fallback extraction classifiers (ArgoCD, Terraform
via `impact/deployment`, GitHub Actions, Dockerfile/Compose, k8s SELECTS,
CloudFormation, Kustomize, Rust impl blocks, JSX component references) and
the truncation-clip accounting they report. It reads only through the
narrow `querycontract.ContentRelationshipReader` port. It must not import
the query root or the parent `entity` package: the root imports this leaf,
so a back-import cycles.

`BuildOutgoingK8sSelectRelationships` and `BuildIncomingK8sSelectRelationships`
are exported only for the query root's staying classifier regression tests;
`ContentRelationshipLimit` is the single-shared content-lookup ceiling the root
regression tests bind instead of copying; satellite and handler consumers use `Builder`.
