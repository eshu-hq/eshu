// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package stepfunctions maps AWS Step Functions metadata into AWS cloud
// collector facts.
//
// The scanner emits reported-confidence state machine and activity resources
// plus relationships for state-machine-to-IAM-role and
// state-machine-to-referenced-resource ARN evidence drawn from Task targets
// inside the state machine definition. Execution input, execution output,
// execution history events, activity task tokens, and literal
// Parameters/ResultPath/ResultSelector/InputPath/OutputPath/Result contents
// from the state machine definition stay outside this package contract;
// mutation APIs such as StartExecution, StopExecution, CreateStateMachine,
// UpdateStateMachine, DeleteStateMachine, SendTaskSuccess, and SendTaskFailure
// are forbidden.
package stepfunctions
