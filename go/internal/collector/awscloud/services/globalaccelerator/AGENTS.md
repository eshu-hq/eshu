# Global Accelerator Service Package

Read these files before editing this package:

1. `README.md`
2. `doc.go`
3. `types.go`
4. `scanner.go`
5. `relationships.go`
6. `awssdk/README.md`

Do not add mutation APIs, BYOIP advertise/withdraw calls, custom-routing traffic
allow/deny calls, or any operation that creates, updates, or deletes a Global
Accelerator resource. The scanner is metadata-only.

The scanner boundary must remain `awscloud.ServiceGlobalAccelerator`. Global
Accelerator is a global-endpoint service whose control plane lives only in
`us-west-2`; tests and runtime scopes use a `us-west-2` boundary and must not
infer application environment or deployment truth.

Endpoint target typing is a correctness contract. Keep `endpointTargetType` in
sync with the join keys downstream correlation expects: ELB v2 load balancers
are keyed by ARN, Elastic IPs by `eipalloc-` allocation id, and EC2 instances by
`i-` instance id. Never hardcode an `arn:aws:` partition; derive `target_arn`
only from an ARN-shaped endpoint id.
