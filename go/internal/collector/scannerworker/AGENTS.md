# scannerworker Agent Notes

- Keep this package contract-only unless a local design doc explicitly moves a
  scanner runtime here.
- Scanner workers emit source facts only. Do not add reducer finding fact kinds
  or graph projection ownership to this package.
- Keep target payloads privacy-safe: raw repository paths, image names, bucket
  keys, and source locators must not appear in retry or dead-letter strings.
- Any runtime implementation must add contention, retry, dead-letter, pprof,
  CPU, memory, queue-age, duration, target-count, and result-count proof before
  review.
