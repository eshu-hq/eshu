# services/cloudwatch/runtimebind

Self-registers the CloudWatch metadata scanner into the `awsruntime` registry
via `init()`. The collector main and the bindings aggregate package both
import this package as a blank import; nothing else in the runtime needs to
change to make CloudWatch reachable.

The binder fails closed when the runtime-provided `RedactionKey` is zero
because the scanner cannot redact customer-tag-named alarm metric dimensions
without it.

## Tests

- `bind_test.go` asserts `awsruntime.LookupBuilder(awscloud.ServiceCloudWatch)`
  returns a non-nil builder after the package is imported.
- It also asserts the builder returns a typed error when `RedactionKey` is
  zero.
