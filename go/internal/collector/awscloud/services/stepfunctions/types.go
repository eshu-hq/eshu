package stepfunctions

import (
	"context"
	"time"
)

// Client lists AWS Step Functions metadata for one claimed account and region.
//
// Runtime adapters translate AWS SDK responses into scanner-owned records.
// Tests use small fakes that satisfy this interface without depending on the
// AWS SDK pagination machinery.
type Client interface {
	ListStateMachines(context.Context) ([]StateMachine, error)
	ListActivities(context.Context) ([]Activity, error)
}

// StateMachine is the metadata-only scanner view of one Step Functions state
// machine. It carries only the metadata that the AWS API reports directly plus
// the structural state graph derived from the definition document.
//
// Execution input, execution output, execution history events, task tokens,
// and literal Parameters/ResultPath/ResultSelector contents must never appear
// in this record. The structural view is intentionally narrow: state names,
// state types, transition edges, and Task Resource ARNs only.
type StateMachine struct {
	ARN            string
	Name           string
	Type           string
	Status         string
	RoleARN        string
	CreationDate   time.Time
	LoggingLevel   string
	TracingEnabled bool
	StartAt        string
	States         []StateNode
	ReferencedARNs []string
	Tags           map[string]string
}

// StateNode is the safe structural view of one state inside a Step Functions
// state machine definition. It carries only the state name, the state type,
// the directed graph edges (Next, End, Default, choice/catch transitions),
// and Task resource ARNs. It never carries Parameters, ResultPath,
// ResultSelector, InputPath, OutputPath, Result, Cause, Error literals, or any
// raw payload from the definition.
type StateNode struct {
	Name        string
	Type        string
	End         bool
	Next        string
	Default     string
	Choices     []string
	CatchNext   []string
	ResourceARN string
}

// Activity is the metadata-only scanner view of one Step Functions activity.
// Activity task tokens, send-task payloads, and task input/output payloads
// must never appear in this record.
type Activity struct {
	ARN          string
	Name         string
	CreationDate time.Time
	Tags         map[string]string
}
