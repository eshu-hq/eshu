# AGENTS — services/cloudwatch/bind

This package wires the CloudWatch scanner into the `runtime` registry
through `init()`. Agents editing this package MUST:

- Keep the binder shape consistent with sibling services (`sqs/bind`,
  `ecs/bind`, `lambda/bind`). The pattern is one
  `runtime.Register` call per service.
- Validate `RedactionKey` before constructing the scanner. Returning a typed
  error is the only acceptable behavior when the key is zero.
- NEVER modify `go/internal/collector/cloud/aws/runtime/registry.go`. The
  registry is intentionally service-package-agnostic; adding a `case` or an
  `import` there is the pre-#764 pattern.
- Update `runtime/bindings/bindings.go` with one alphabetical blank import
  line. There is no want-list to edit: the supported-service guard is derived
  from the `services/<svc>/bind/` directories plus the `bindings.go`
  imports (see `runtime/internal/guardset`).
