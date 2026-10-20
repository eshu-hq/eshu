# Global Accelerator AWS SDK Adapter

Read these files before editing this package:

1. `README.md`
2. `doc.go`
3. `client.go`
4. `mapper.go`
5. `exclusion_test.go`
6. `../README.md`

Allowed API calls are `ListAccelerators`, `ListListeners`,
`ListEndpointGroups`, and `ListTagsForResource`. Do not add any mutation call
(Create/Update/Delete/Provision/Advertise/Withdraw/Add/Remove/Allow/Deny), the
custom-routing add/remove endpoint calls, or BYOIP advertise/withdraw without a
new issue and an evidence note. `exclusion_test.go` enforces this.

Keep the SDK client region pinned to `us-west-2` in `NewClient`. The Global
Accelerator control plane is reachable only there; removing the pin breaks
non-us-west-2 claims silently.

Keep pagination bounded and observable through `recordAPICall`. New AWS calls
must add focused fake-client tests that prove the request shape and mapping.
