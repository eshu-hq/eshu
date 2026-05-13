// Package workflow defines the durable contracts for the workflow control
// plane: runs, work items, claims, collector instances, completeness states,
// and the reducer-facing phase contract per collector family.
//
// Types here are storage-neutral value contracts with Validate methods that
// enforce identity, status-lifecycle, and timestamp invariants. ControlStore
// is the durable surface implemented by storage/postgres. ReconcileRunProgress
// derives run status and completeness rows deterministically from bounded
// collector progress and reducer phase publications, including blocked
// completeness when terminal collector failures appear. The family fairness
// scheduler chooses the next claim target across enabled collector instances.
// Terraform state collector instances also validate their discovery config
// before reaching durable storage: they must enable graph discovery, explicit
// seeds, or local repo limits, and S3 seeds require bucket, key, region, and an
// AWS role ARN. OCI registry collector instances validate bounded repository
// targets, and package registry collector instances validate bounded package
// metadata targets and known document formats, so the coordinator can plan
// claimable work items without opening provider connections.
package workflow
