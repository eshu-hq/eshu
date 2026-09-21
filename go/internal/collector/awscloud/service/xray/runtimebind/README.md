# services/xray/runtimebind

Self-registers the X-Ray configuration scanner into the `awsruntime` registry
via `init()`. The collector main and the bindings aggregate package both import
this package as a blank import; nothing else in the runtime needs to change to
make X-Ray reachable.

The binder sets no `RequiresRedactionKey` flag: the X-Ray configuration scanner
emits no secret-shaped fields, so it builds without an `ESHU_AWS_REDACTION_KEY`.

## Tests

- `bind_test.go` asserts `awsruntime.LookupBuilder(awscloud.ServiceXRay)`
  returns a non-nil builder after the package is imported, and that the builder
  succeeds without a redaction key.
