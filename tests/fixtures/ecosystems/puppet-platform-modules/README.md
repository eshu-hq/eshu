# puppet-platform-modules

B-7 corpus fixture. An r10k `Puppetfile` whose `acme-base` module is sourced
from the `deployable-source` repository, declared with an explicit git source:

```ruby
mod 'acme-base',
  :git => 'https://github.com/acme/deployable-source',
  :ref => 'v1.0.0'
```

This produces a `PUPPET_MODULE_REFERENCE` evidence fact that resolves to the
in-corpus `deployable-source` repository, materialising a
`(:Repository)-[:DEPENDS_ON]->(:Repository)` edge. The golden-corpus gate's
`rc-32` asserts it as an evidence-filtered required correlation so the Puppet
module-dependency verb is isolated from the package-consumption (`rc-3`) and
Ansible role (`rc-30`) `DEPENDS_ON` edges (see the snapshot's `evidence_kinds`
predicate).

Forge-only `mod` entries (no `:git` source) are intentionally inert: the emitter
resolves only modules that name an explicit git URL, so it never fabricates a
repository edge for a Puppet Forge slug.

No proprietary data: all identifiers are synthetic (`acme` org, generic
`acme-base` module).
