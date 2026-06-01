# imageanalyzer Agent Notes

- Keep this package focused on scanner-worker image/rootfs component extraction.
- Do not add registry pulls, credentials, reducer findings, graph writes, or
  query handlers here.
- Preserve image reference, digest, evidence source, package manager, package
  name, installed version, distro, unsupported state, and extraction reason in
  emitted source facts.
- Never emit OS package facts without rootfs or layer package database proof.
- Keep local rootfs and layer paths out of retry, dead-letter, metric, and log
  payloads.
