// Package cloudruntime carries helper Go for the AWS cloud-runtime drift
// correlation pack.
//
// The package compares one ARN's AWS-observed resource, Terraform-state
// resource, and Terraform-config resource views before
// engine.Evaluate(rules.AWSCloudRuntimeDriftRulePack(), ...) runs. It emits
// candidates for two conservative findings only: cloud resources with no
// Terraform-state backing and cloud resources that have state backing but no
// current config declaration. It does not write graph truth or query any
// backend; reducer wiring decides when to persist or publish the evaluation.
package cloudruntime
