# AGENTS — services/cloudwatch/awssdk

This adapter is the contract surface between the AWS SDK v2 and the
`cloudwatch.Client` interface. Agents editing this package MUST:

- Treat the local `apiClient` interface as a security boundary. Adding
  `GetDashboard` or any mutation method (`Put*`, `Delete*`, `Enable*`,
  `Disable*`, `Start*`, `Stop*`, `SetAlarmState`) is a rule violation and the
  existing tests fail if any forbidden method appears.
- Persist only metadata. Dashboard body JSON and Contributor Insights rule
  definitions are NEVER mapped onto the scanner-owned model.
- Use pagination through `NextToken` for every list call. Do not assume a
  single page.
- Record every API call through `recordAPICall` so the runtime gets
  per-operation counters, throttle attribution, and tracer spans.

## Layout

- `client.go`: the `Client` adapter, paginator loops, and the narrow
  `apiClient` interface.
- `mapper.go`: SDK-to-scanner-model mapping helpers.
- `client_test.go`: fake apiClient that records every call name and asserts
  no forbidden call was made; reflection over the `apiClient` interface to
  prove its shape.
