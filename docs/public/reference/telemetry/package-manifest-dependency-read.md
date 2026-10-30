# Package Manifest Dependency Read Signals

A package-registry reducer intent loads the active Git manifest dependency
rows for its ecosystem and package names through
`ListActivePackageManifestDependencyFacts`, 500 rows per keyset page. These
signals cover one whole read, across all of its pages (#9586).

The `read` label names the query text that ran: `plain` for every ecosystem
except Maven, `maven_family` for the Maven query that also reaches Gradle and
sbt rows. Only completed reads record a sample; a failed read surfaces as the
reducer intent's failure.

- `eshu_dp_package_manifest_dependency_read_seconds`: histogram of one read's
  wall time, labeled `read`.
- `eshu_dp_package_manifest_dependency_read_pages`: histogram of the keyset
  pages one read issued, labeled `read`.

Read them together. Wall time that grows with pages is the expected cost of a
large name set. Wall time that climbs while pages hold means each page got
slower. That is the shape #9586 fixed: once PostgreSQL switched a cached
statement to its generic plan, each page cost seconds even with no rows to
return. Each page now runs on the unnamed statement with a custom plan.
