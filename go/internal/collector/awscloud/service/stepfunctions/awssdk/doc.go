// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Step Functions client into the
// metadata-only Step Functions scanner interface.
//
// The adapter uses ListStateMachines, DescribeStateMachine, ListActivities,
// and ListTagsForResource. It intentionally excludes
// StartExecution, StopExecution, CreateStateMachine, UpdateStateMachine,
// DeleteStateMachine, SendTaskSuccess, SendTaskFailure, execution input and
// output persistence, execution history events, activity task tokens, and
// literal Parameters/ResultPath/ResultSelector/InputPath/OutputPath/Result
// contents from the state machine definition. The definition is read only as
// state names, state types, structural transitions, and Task Resource ARNs.
package awssdk
