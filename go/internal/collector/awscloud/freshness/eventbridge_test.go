package freshness

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestNormalizeEventBridgeConfigChangeTargetsServiceTuple(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"version": "0",
		"id": "config-event-1",
		"detail-type": "Config Configuration Item Change",
		"source": "aws.config",
		"account": "123456789012",
		"region": "us-east-1",
		"time": "2026-05-15T10:11:12Z",
		"resources": ["arn:aws:lambda:us-east-1:123456789012:function:orders-api"],
		"detail": {
			"configurationItem": {
				"awsAccountId": "123456789012",
				"awsRegion": "us-east-1",
				"resourceType": "AWS::Lambda::Function",
				"resourceId": "orders-api",
				"configurationItemCaptureTime": "2026-05-15T10:10:59Z"
			}
		}
	}`)

	trigger, err := NormalizeEventBridge(payload)
	if err != nil {
		t.Fatalf("NormalizeEventBridge() error = %v, want nil", err)
	}
	if trigger.Kind != EventKindConfigChange {
		t.Fatalf("Kind = %q, want %q", trigger.Kind, EventKindConfigChange)
	}
	if trigger.EventID != "config-event-1" {
		t.Fatalf("EventID = %q, want config-event-1", trigger.EventID)
	}
	if trigger.AccountID != "123456789012" {
		t.Fatalf("AccountID = %q, want 123456789012", trigger.AccountID)
	}
	if trigger.Region != "us-east-1" {
		t.Fatalf("Region = %q, want us-east-1", trigger.Region)
	}
	if trigger.ServiceKind != awscloud.ServiceLambda {
		t.Fatalf("ServiceKind = %q, want %q", trigger.ServiceKind, awscloud.ServiceLambda)
	}
	if trigger.ResourceType != "AWS::Lambda::Function" {
		t.Fatalf("ResourceType = %q, want AWS::Lambda::Function", trigger.ResourceType)
	}
	if trigger.ResourceID != "orders-api" {
		t.Fatalf("ResourceID = %q, want orders-api", trigger.ResourceID)
	}
	wantObserved := time.Date(2026, 5, 15, 10, 10, 59, 0, time.UTC)
	if !trigger.ObservedAt.Equal(wantObserved) {
		t.Fatalf("ObservedAt = %s, want %s", trigger.ObservedAt, wantObserved)
	}
}

func TestNormalizeEventBridgeCloudTrailAPITargetsServiceTuple(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"version": "0",
		"id": "cloudtrail-event-1",
		"detail-type": "AWS API Call via CloudTrail",
		"source": "aws.ec2",
		"account": "123456789012",
		"region": "us-west-2",
		"time": "2026-05-15T11:12:13Z",
		"detail": {
			"eventSource": "ec2.amazonaws.com",
			"eventName": "AuthorizeSecurityGroupIngress",
			"requestParameters": {
				"groupId": "sg-0123456789abcdef0"
			}
		}
	}`)

	trigger, err := NormalizeEventBridge(payload)
	if err != nil {
		t.Fatalf("NormalizeEventBridge() error = %v, want nil", err)
	}
	if trigger.Kind != EventKindCloudTrailAPI {
		t.Fatalf("Kind = %q, want %q", trigger.Kind, EventKindCloudTrailAPI)
	}
	if trigger.ServiceKind != awscloud.ServiceEC2 {
		t.Fatalf("ServiceKind = %q, want %q", trigger.ServiceKind, awscloud.ServiceEC2)
	}
	if trigger.Region != "us-west-2" {
		t.Fatalf("Region = %q, want us-west-2", trigger.Region)
	}
	if trigger.ResourceID != "sg-0123456789abcdef0" {
		t.Fatalf("ResourceID = %q, want sg-0123456789abcdef0", trigger.ResourceID)
	}
	wantObserved := time.Date(2026, 5, 15, 11, 12, 13, 0, time.UTC)
	if !trigger.ObservedAt.Equal(wantObserved) {
		t.Fatalf("ObservedAt = %s, want %s", trigger.ObservedAt, wantObserved)
	}
}

func TestNormalizeEventBridgeRejectsUnsupportedService(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"id": "cloudtrail-event-2",
		"detail-type": "AWS API Call via CloudTrail",
		"source": "aws.support",
		"account": "123456789012",
		"region": "us-east-1",
		"time": "2026-05-15T11:12:13Z",
		"detail": {
			"eventSource": "support.amazonaws.com",
			"eventName": "CreateCase"
		}
	}`)

	if _, err := NormalizeEventBridge(payload); err == nil {
		t.Fatal("NormalizeEventBridge() error = nil, want unsupported service error")
	}
}
