# Shield Advanced AWS SDK Adapter

Read these files before editing this package:

1. `README.md`
2. `doc.go`
3. `client.go`
4. `mapping.go`
5. `exclusion_test.go`
6. `../README.md`

Allowed API calls are `ListProtections`, `DescribeSubscription`, and
`GetSubscriptionState`. Do not add any mutation call
(Create/Update/Delete/Associate/Disassociate/Enable/Disable/Tag/Untag) or the
DRT/proactive-engagement/emergency-contact mutators without a new issue and an
evidence note. `exclusion_test.go` enforces this.

Keep the adapter metadata only. Map the subscription ARN, state, and auto-renew
flag only. Do not map or persist `SubscriptionLimits`,
`TimeCommitmentInSeconds`, `StartTime`, `EndTime`, or
`ProactiveEngagementStatus`: those are billing/operational detail outside the
scanner contract.

Keep the SDK client region pinned to `us-east-1` in `NewClient`. The Shield
control plane is reachable only there; removing the pin breaks non-us-east-1
claims silently.

Treat a missing subscription as nil, not an error. `DescribeSubscription`
returns `ResourceNotFoundException` for accounts without Shield Advanced; the
adapter must keep mapping that to a nil subscription.

Keep pagination bounded and observable through `recordAPICall`. New AWS calls
must add focused fake-client tests that prove the request shape and mapping.
