# AGENTS.md - internal/collector/kuberneteslive/clientgo guidance

## Read First

1. `go/internal/collector/kuberneteslive/clientgo/README.md` - adapter contract
2. `go/internal/collector/kuberneteslive/clientgo/config.go` - auth modes
3. `go/internal/collector/kuberneteslive/clientgo/client.go` - list + mapping
4. `go/internal/collector/kuberneteslive/clientgo/client_workload_kinds.go` -
   StatefulSet/DaemonSet/Job/CronJob list + mapping
5. `go/internal/collector/kuberneteslive/clientgo/identity_rbac.go` -
   ServiceAccount and RBAC list + mapping
6. `go/internal/collector/kuberneteslive/client.go` - the neutral Client interface

## Invariants This Package Enforces

- This is the ONLY package allowed to import client-go and `k8s.io/api*`. Keep
  all Kubernetes typed-API dependence here.
- READ-ONLY. Only issue `list` calls. Never add a create, update, patch,
  delete, exec, attach, portforward, or log call.
- METADATA-ONLY. When mapping objects, never copy `env.Value`, Secret data,
  ConfigMap data, or any value-bearing field. Map image refs, ports, env var
  NAMES, service account, selector, labels, ServiceAccount annotation keys,
  automount posture, bounded secret-reference counts, RBAC rule summaries, and
  RBAC subject identity fields needed for downstream fingerprinting only.
  Never copy ServiceAccount referenced Secret names, token values, RBAC
  `resourceNames`, or `nonResourceURLs` as cleartext payload fields.
- A `Forbidden` list becomes a partial result with reason
  `WarningForbiddenResource`; a mid-stream failure after pages becomes
  `WarningPartialList`. Do not turn a forbidden list into an empty success.
- Paginate with `Limit` and `Continue`; do not issue unbounded single-shot
  lists.

## Common Changes And How To Scope Them

- Add a resource family by adding a `List*` method plus a fake-clientset test
  that asserts the neutral mapping and the forbidden->partial path.
- Add an auth mode by extending `AuthMode` and `AuthConfig.RESTConfig` with a
  validation test; keep credentials out of logs and fact payloads.

## Anti-Patterns

- Leaking client-go types across the `kuberneteslive.Client` boundary.
- Reading any value-bearing field (env value, secret/configmap data, logs).
- Returning a hard error for a forbidden list instead of a partial warning.
