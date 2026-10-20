# AGENTS — services/cloudwatch/runtimebind

This package wires the CloudWatch scanner into the `awsruntime` registry
through `init()`. Agents editing this package MUST:

- Keep the binder shape consistent with sibling services (`sqs/runtimebind`,
  `ecs/runtimebind`, `lambda/runtimebind`). The pattern is one
  `awsruntime.Register` call per service.
- Validate `RedactionKey` before constructing the scanner. Returning a typed
  error is the only acceptable behavior when the key is zero.
- NEVER modify `go/internal/collector/awscloud/awsruntime/registry.go`. The
  registry is intentionally service-package-agnostic; adding a `case` or an
  `import` there is the pre-#764 pattern.
- Update `awsruntime/bindings/bindings.go` with one alphabetical blank import
  line. There is no want-list to edit: the supported-service guard is derived
  from the `services/<svc>/runtimebind/` directories plus the `bindings.go`
  imports (see `awsruntime/internal/guardset`).
