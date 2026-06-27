# chef-cookbooks

B-7 corpus fixture. A Berkshelf `Berksfile` whose `acme-base` cookbook is sourced
from the `deployable-source` repository, declared with an explicit git source:

```ruby
cookbook 'acme-base',
  git: 'https://github.com/acme/deployable-source',
  branch: 'main'
```

This produces a `CHEF_COOKBOOK_DEPENDENCY` evidence fact that resolves to the
in-corpus `deployable-source` repository, materialising a
`(:Repository)-[:DEPENDS_ON]->(:Repository)` edge. The golden-corpus gate's
`rc-33` asserts it as an evidence-filtered required correlation so the Chef
cookbook-dependency verb is isolated from the package-consumption (`rc-3`),
Ansible role (`rc-30`) and Puppet module (`rc-32`) `DEPENDS_ON` edges (see the
snapshot's `evidence_kinds` predicate).

Supermarket-only `cookbook` entries (no `git:` source) are intentionally inert:
the emitter resolves only cookbooks that name an explicit git URL, so it never
fabricates a repository edge for a Chef Supermarket version constraint.

No proprietary data: all identifiers are synthetic (`acme` org, generic
`acme-base` cookbook).
