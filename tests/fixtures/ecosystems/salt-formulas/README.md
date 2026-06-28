# salt-formulas

B-7 corpus fixture. A Salt master config (`master.yaml`) whose `gitfs_remotes`
list sources the `deployable-source` formula from the in-corpus
`deployable-source` repository via gitfs:

```yaml
fileserver_backend:
  - gitfs

gitfs_provider: pygit2

gitfs_remotes:
  - https://github.com/acme/deployable-source
  - https://github.com/acme/network-formulas:
      - root: salt
      - base: main
```

This produces a `SALT_FORMULA_REFERENCE` evidence fact that resolves to the
in-corpus `deployable-source` repository, materialising a
`(:Repository)-[:DEPENDS_ON]->(:Repository)` edge. The golden-corpus gate's
`rc-36` asserts it as an evidence-filtered required correlation so the Salt
gitfs formula-dependency verb is isolated from the package-consumption (`rc-3`),
Ansible role (`rc-30`), Puppet module (`rc-32`) and Chef cookbook (`rc-33`)
`DEPENDS_ON` edges (see the snapshot's `evidence_kinds` predicate).

The `network-formulas` remote (single-key map form) demonstrates the per-remote
options syntax Salt supports; it is not staged as a corpus repository, so it
does not resolve to an edge. Only `deployable-source` is in-corpus, so the
fixture contributes exactly one Salt-sourced `DEPENDS_ON` edge.

Both gitfs entry shapes are exercised: a plain URL string and a single-key map
whose key is the URL with nested per-remote options. The emitter reads the map
key as the formula repository URL.

No proprietary data: all identifiers are synthetic (`acme` org, generic
`network-formulas` formula).
