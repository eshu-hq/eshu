# AGENTS.md - services/sns/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are `ListTopics`, `GetTopicAttributes`,
  `ListTagsForResource`, and `ListSubscriptionsByTopic`.
- Wrap every paginator page and point read in `recordAPICall`.
- Do not persist `Policy`, `DeliveryPolicy`, `EffectiveDeliveryPolicy`, or
  `DataProtectionPolicy`; do not persist raw non-ARN subscription endpoints.
- Do not call `Publish`, `Subscribe`, `Unsubscribe`, `SetTopicAttributes`,
  `PutDataProtectionPolicy`, credential, STS, graph, or reducer APIs.
- Keep topic ARNs, topic names, tags, subscription ARNs, endpoints, page tokens,
  policy JSON, and raw AWS errors out of metric labels.
