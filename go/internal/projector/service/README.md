# Service projector namespace

This documentation-only package groups projector intent builders whose source
evidence belongs to the service domain. It owns no runtime assembly, queue,
retry, storage, graph, or telemetry behavior.

The `catalog` leaf recognizes service-catalog facts for one scope generation
and builds the reducer intent that asks the reducer to correlate those catalog
declarations against repository and deployment truth. Root
`internal/projector` owns lookup construction, invocation order, and enqueue;
the reducer's `DomainServiceCatalogCorrelation` handler owns the correlation
decisions and every write.

This namespace is a readable ownership seam, not an independently extractable
service. A future repository split still needs explicit replacements for the
repository-internal fact, intent, and reducer-domain contracts.

### Move record (#6627)

No-Regression Evidence (#6627 service nesting): base `cef5e2506`,
backend go1.27.1 darwin/arm64; new docs-only namespace parent created by this
move holds no runtime code, so there is no trigger, value, or fan-out change
to regress. Same build/vet/recursive-test record as the `catalog` leaf above;
B-12 replay reported 437/437 PASS in the move lane with CI required-gates as
the blocking authority, and B-7 was not run locally (Docker daemon
unreachable), leaving CI as the blocking authority there. Rename-only
relocation; no benchmark delta exists to measure.

No-Observability-Change (#6627 service nesting): this package emits no
signal directly; intent volume and reducer execution stay covered by the
instruments named in the leaf section, unchanged.
