// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceStepFunctions identifies the regional AWS Step Functions metadata
	// scan slice.
	ServiceStepFunctions = "stepfunctions"
)

const (
	// ResourceTypeStepFunctionsStateMachine identifies a Step Functions state
	// machine metadata resource.
	ResourceTypeStepFunctionsStateMachine = "aws_stepfunctions_state_machine"
	// ResourceTypeStepFunctionsActivity identifies a Step Functions activity
	// metadata resource.
	ResourceTypeStepFunctionsActivity = "aws_stepfunctions_activity"
)

const (
	// RelationshipStepFunctionsStateMachineUsesIAMRole records a state
	// machine's reported execution-role dependency.
	RelationshipStepFunctionsStateMachineUsesIAMRole = "stepfunctions_state_machine_uses_iam_role"
	// RelationshipStepFunctionsStateMachineReferencesResource records evidence
	// that a state machine definition references an ARN-addressable resource as
	// a Task target.
	RelationshipStepFunctionsStateMachineReferencesResource = "stepfunctions_state_machine_references_resource"
)
