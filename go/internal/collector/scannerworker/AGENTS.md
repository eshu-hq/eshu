# scannerworker Agent Notes

- Keep this package focused on the hosted scanner-worker boundary: claim input,
  analyzer ports, output validation, safe failure payloads, and the claim loop.
  Concrete SBOM, image, secret, license, OS package, source, and
  misconfiguration analyzers belong behind the `Analyzer` port unless a design
  doc moves one here.
- Scanner workers emit source facts only. Do not add reducer finding fact kinds
  or graph projection ownership to this package.
- Keep target payloads privacy-safe: raw repository paths, image names, bucket
  keys, and source locators must not appear in retry or dead-letter strings.
- Runtime changes must preserve contention, retry, dead-letter, pprof, CPU,
  memory, queue-age, duration, target-count, and result-count proof before
  review.
