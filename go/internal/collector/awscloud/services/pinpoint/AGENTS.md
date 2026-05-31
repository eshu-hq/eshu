# AGENTS.md - internal/collector/awscloud/services/pinpoint guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Pinpoint domain types.
3. `scanner.go` - application, segment, and channel resource and relationship
   emission, plus service_kind canonicalization.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, SES identity-name extraction, and
   scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Pinpoint API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read or persist endpoint records, endpoint addresses, phone numbers, the
  email from-address, message or template content, segment targeting dimensions
  or segment-group criteria, the import S3 URL, or the import external id. Only
  presence/format/aggregate-size of an S3 import are metadata.
- The application node publishes its resource_id as the application id (fallback
  ARN, then name). Key the application-to-segment and channel-in-application
  edges on that exact value so they join the application node.
- Source a segment's own edge on the segment ARN (fallback to the
  application-qualified segment id), the resource_id the segment node publishes.
- Key a channel by `<application-id>/<channel-type>`; a Pinpoint channel has no
  AWS-assigned ARN.
- Emit the email-channel-to-SES-identity edge only when Pinpoint reports an SES
  `:identity/<name>` ARN. Key the bare identity NAME (matching the SES
  email-identity scanner's published resource_id). Do NOT set `target_arn`,
  since the SES node is name-keyed; preserve the reported identity ARN in the
  `ses_identity_arn` edge attribute instead. Skip the edge when the value is not
  an SES identity ARN.
- Emit the email-channel-to-SES-configuration-set edge only when a configuration
  set is reported; key the configuration set NAME (matching the SES
  configuration-set scanner's published resource_id).
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Canonicalize `service_kind` by switching on `strings.TrimSpace(...)` and
  writing `awscloud.ServicePinpoint` back on the merged empty/matched case.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from Pinpoint names or tags.
- Keep Pinpoint resource ARNs, names, tags, and AWS error payloads out of metric
  labels.

## Common Changes

- Add a new Pinpoint metadata field by extending the scanner-owned type, writing
  a focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry endpoint, address, or message
  content, leave it out of the scanner contract.
- Add new relationship evidence only when the Pinpoint API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read endpoint records, send messages, read message/template content,
  or call any Pinpoint mutation API.
- Do not persist the email from-address, the import S3 URL, the import external
  id, or segment targeting criteria values.
- Do not resolve Pinpoint names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
