# services/cloudwatch/awssdk

AWS SDK v2 adapter that satisfies `cloudwatch.Client`. The adapter is built
around a narrow `apiClient` interface so the compiler enforces the
metadata-only contract:

- `DescribeAlarms` reads metric alarms and composite alarms.
- `ListDashboards` reads dashboard identity (name, ARN, last modified, size).
  `GetDashboard` is NOT included; the dashboard body JSON is unreachable.
- `DescribeInsightRules` reads Contributor Insights rule identity. The rule
  definition body is dropped on the floor by the mapper.
- `ListMetricStreams` and `GetMetricStream` read metric stream metadata.
- `ListTagsForResource` reads tags.

Mutation APIs (`PutMetricAlarm`, `DeleteAlarms`, `PutCompositeAlarm`,
`PutDashboard`, `DeleteDashboards`, `EnableAlarmActions`,
`DisableAlarmActions`, `SetAlarmState`, `PutInsightRule`,
`DeleteInsightRules`, `StartMetricStreams`, `StopMetricStreams`,
`PutMetricData`) are not part of the apiClient interface. Any call to them
would not compile.

The companion test reflects the apiClient interface at runtime and asserts:

- The interface contains only the allowed read methods.
- During a List/Describe flow, the fake records every call name and none of
  the forbidden names appear.
