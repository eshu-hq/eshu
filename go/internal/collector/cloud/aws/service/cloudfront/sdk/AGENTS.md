# CloudFront AWS SDK Adapter

Read these files before editing this package:

1. `README.md`
2. `doc.go`
3. `client.go`
4. `mapper.go`
5. `../README.md`

Allowed API calls are `ListDistributions` and `ListTagsForResource`.
Do not add `GetDistributionConfig`, mutation calls, object reads, policy
document fetches, certificate body reads, private-key handling, or origin custom
header value persistence without a new issue and evidence note.

Keep pagination bounded and observable through `recordAPICall`. New AWS calls
must add focused fake-client tests that prove the request shape and mapping.
