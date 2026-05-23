# AGENTS.md - services/ecr

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, and `awssdk/README.md`
before editing this service.

## Mandatory Rules

- Keep ECR AWS access behind `Client`; the scanner package must not import the
  AWS SDK.
- Emit repository resources, lifecycle-policy child resources, and
  `aws_image_reference` facts only.
- Keep untagged image digest evidence visible with an empty tag.
- Do not infer workload, environment, repository ownership, deployable-unit, or
  vulnerability impact truth from repositories, tags, images, or accounts.
- Treat lifecycle policy JSON as payload evidence; never use it as a metric
  label.
- Keep repository names, ARNs, image digests, tags, policy JSON, raw AWS
  errors, and page tokens out of metric labels.
