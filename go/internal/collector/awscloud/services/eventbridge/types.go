package eventbridge

import (
	"context"
	"time"
)

// Client lists EventBridge metadata for one claimed account and region.
type Client interface {
	ListEventBuses(context.Context) ([]EventBus, error)
}

// EventBus is the metadata-only scanner view of an EventBridge event bus.
type EventBus struct {
	ARN              string
	Name             string
	Description      string
	CreationTime     time.Time
	LastModifiedTime time.Time
	Tags             map[string]string
	Rules            []Rule
}

// Rule is the metadata-only scanner view of an EventBridge rule.
type Rule struct {
	ARN                string
	Name               string
	EventBusName       string
	Description        string
	EventPattern       string
	ManagedBy          string
	RoleARN            string
	ScheduleExpression string
	State              string
	CreatedBy          string
	Tags               map[string]string
	Targets            []Target
}

// Target is the metadata-only scanner view of an EventBridge rule target.
type Target struct {
	ID                       string
	ARN                      string
	RoleARN                  string
	DeadLetterARN            string
	MaximumEventAgeInSeconds int32
	MaximumRetryAttempts     int32
}
