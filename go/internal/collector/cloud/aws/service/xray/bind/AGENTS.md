# AGENTS — services/xray/bind

This package wires the X-Ray scanner into the `runtime` registry through
`init()`. Agents editing this package MUST:

- Keep the binder shape consistent with sibling services (`kms/bind`,
  `cloudwatch/bind`). The pattern is one `runtime.Register` call per
  service.
- Leave `RequiresRedactionKey` unset. The X-Ray configuration scanner carries no
  secret-shaped fields. Do not add a redaction-key requirement without a real
  secret-bearing field on the scanner-owned model.
- NEVER modify `go/internal/collector/cloud/aws/runtime/registry.go`. The
  registry is intentionally service-package-agnostic; adding a `case` or an
  `import` there is the pre-#764 pattern.
- Update `runtime/bindings/bindings.go` with one alphabetical blank import
  line only. There is no want-list to edit: the supported-service guard is
  derived from the `services/<svc>/bind/` directories plus the
  `bindings.go` imports. Do not gofmt-reorder that file; it carries pre-existing
  `merge=union` disorder and append-only is the rule.
