# AGENTS.md - internal/collector/awscloud/services/lightsail guidance

## Read First

1. `README.md` - package purpose, exported surface, identity keying, and
   invariants.
2. `types.go` - scanner-owned Lightsail domain types.
3. `scanner.go` - instance, database, load balancer, disk, static IP, and
   relationship emission.
4. `relationships.go` - Lightsail-internal relationship emission rules.
5. `helpers.go` - scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Lightsail API access behind `Client`; do not import the AWS SDK into
  this package.
- Never create, delete, reboot, start, stop, or snapshot a Lightsail resource,
  never attach or detach disks or static IPs, and never wire any `Create*`,
  `Delete*`, `Reboot*`, `Start*`, `Stop*`, `*Snapshot`, `Attach*`, or
  `Detach*` API.
- Never read or persist instance access details, default key-pair private
  keys, or database master passwords.
- Key every node resource_id and every relationship endpoint on the bare
  Lightsail resource name. The source of each internal edge must equal the
  source node's resource_id and the target must equal the target node's
  resource_id, or the edge dangles.
- Use Lightsail resource ARNs exactly as the API reports them. Never synthesize
  an ARN and never hardcode `arn:aws:`; if a synthesized ARN ever becomes
  necessary, derive the partition with `awscloud.PartitionForBoundary` /
  `PartitionFromARN`.
- Emit load-balancer-to-instance edges once per distinct attached instance
  name; collapse duplicates. Emit instance-to-disk and instance-to-static-IP
  edges only when AWS reports a non-empty attachment target.
- Never key on a list index or API ordering; identity is the stable Lightsail
  name.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from Lightsail names or
  tags.
- Keep Lightsail resource ARNs, names, IP addresses, tags, and AWS error
  payloads out of metric labels.

## Common Changes

- Add a new Lightsail metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry credential or
  access-secret material, leave it out of the scanner contract until an ADR
  documents a sanitized exception.
- Add new relationship evidence only when the Lightsail API reports both sides
  directly and the target identity matches a published node resource_id (a bare
  Lightsail name or an ARN-keyed family).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not mutate any Lightsail resource or call any Lightsail mutation,
  attach/detach, or lifecycle API.
- Do not call `GetInstanceAccessDetails`, `DownloadDefaultKeyPair`,
  `GetRelationalDatabaseMasterUserPassword`, or any API that retrieves
  credential or access-secret material.
- Do not change the node or edge keying away from the bare Lightsail name
  without re-proving every internal edge still joins its target node.
- Do not resolve Lightsail names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
