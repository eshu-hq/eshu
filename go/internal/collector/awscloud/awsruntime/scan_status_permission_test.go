// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceClassifiesDeniedSmithyAPIErrorsAsTerminalPermissionGaps(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		serviceKind string
		errorCode   string
		wantClass   string
	}{
		{
			name:        "access denied",
			serviceKind: awscloud.ServiceIAM,
			errorCode:   "AccessDenied",
			wantClass:   FailureClassPermissionDenied,
		},
		{
			name:        "unauthorized operation",
			serviceKind: awscloud.ServiceEC2,
			errorCode:   "UnauthorizedOperation",
			wantClass:   FailureClassPermissionDenied,
		},
		{
			name:        "unsupported operation",
			serviceKind: awscloud.ServiceKMS,
			errorCode:   "UnsupportedOperationException",
			wantClass:   FailureClassUnsupportedPermission,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
			item := awsWorkItemForService(now, tc.serviceKind)
			statusStore := &stubScanStatusStore{}
			source := ClaimedSource{
				Config: Config{
					CollectorInstanceID: item.CollectorInstanceID,
					Targets: []TargetScope{{
						AccountID:       "123456789012",
						AllowedRegions:  []string{"us-east-1"},
						AllowedServices: []string{tc.serviceKind},
						Credentials: CredentialConfig{
							Mode: CredentialModeLocalWorkloadIdentity,
						},
					}},
				},
				Credentials: &stubCredentialProvider{lease: &stubCredentialLease{}},
				Scanners: &stubScannerFactory{scanner: stubScanner{
					apiEvents: []stubAPICallEvent{{
						operation: "ListMetadata",
						result:    "error",
						throttled: true,
					}},
					err: &smithy.GenericAPIError{
						Code:    tc.errorCode,
						Message: "not authorized to read this service scope",
					},
				}},
				Clock:      func() time.Time { return now },
				ScanStatus: statusStore,
			}

			_, _, err := source.NextClaimed(context.Background(), item)
			if err == nil {
				t.Fatal("NextClaimed() err = nil, want terminal permission error")
			}
			var classified interface{ FailureClass() string }
			if !errors.As(err, &classified) || classified.FailureClass() != tc.wantClass {
				got := ""
				if classified != nil {
					got = classified.FailureClass()
				}
				t.Fatalf("FailureClass() = %q, want %q (err=%v)", got, tc.wantClass, err)
			}
			var terminal interface{ TerminalFailure() bool }
			if !errors.As(err, &terminal) || !terminal.TerminalFailure() {
				t.Fatalf("TerminalFailure() = false, want true (err=%v)", err)
			}
			if len(statusStore.observations) != 1 {
				t.Fatalf("ObserveAWSScan calls = %d, want 1", len(statusStore.observations))
			}
			observation := statusStore.observations[0]
			if observation.Status != awscloud.ScanStatusFailed {
				t.Fatalf("status = %q, want failed", observation.Status)
			}
			if observation.FailureClass != tc.wantClass {
				t.Fatalf("status failure class = %q, want %q", observation.FailureClass, tc.wantClass)
			}
			if observation.FailureMessage == "" {
				t.Fatal("status failure message is blank, want operator-readable permission gap")
			}
		})
	}
}

func TestClaimedSourceKeepsTransportFailureRetryable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 6, 10, 30, 0, 0, time.UTC)
	item := awsWorkItemForService(now, awscloud.ServiceAPIGateway)
	statusStore := &stubScanStatusStore{}
	source := ClaimedSource{
		Config: Config{
			CollectorInstanceID: item.CollectorInstanceID,
			Targets: []TargetScope{{
				AccountID:       "123456789012",
				AllowedRegions:  []string{"us-east-1"},
				AllowedServices: []string{awscloud.ServiceAPIGateway},
				Credentials: CredentialConfig{
					Mode: CredentialModeLocalWorkloadIdentity,
				},
			}},
		},
		Credentials: &stubCredentialProvider{lease: &stubCredentialLease{}},
		Scanners: &stubScannerFactory{scanner: stubScanner{
			err: errors.New("request send failed: dial tcp: i/o timeout"),
		}},
		Clock:      func() time.Time { return now },
		ScanStatus: statusStore,
	}

	_, _, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() err = nil, want retryable transport error")
	}
	var terminal interface{ TerminalFailure() bool }
	if errors.As(err, &terminal) && terminal.TerminalFailure() {
		t.Fatalf("TerminalFailure() = true, want retryable transport failure (err=%v)", err)
	}
	if len(statusStore.observations) != 1 {
		t.Fatalf("ObserveAWSScan calls = %d, want 1", len(statusStore.observations))
	}
	observation := statusStore.observations[0]
	if observation.Status != awscloud.ScanStatusFailed {
		t.Fatalf("status = %q, want failed", observation.Status)
	}
	if observation.FailureClass != "collect_failure" {
		t.Fatalf("failure class = %q, want collect_failure", observation.FailureClass)
	}
}

func awsWorkItemForService(now time.Time, serviceKind string) workflow.WorkItem {
	item := awsWorkItem(now)
	item.WorkItemID = "aws:collector-1:run-1:123456789012:us-east-1:" + serviceKind
	item.ScopeID = "aws:123456789012:us-east-1:" + serviceKind
	item.AcceptanceUnitID = `{"account_id":"123456789012","region":"us-east-1","service_kind":"` + serviceKind + `"}`
	item.SourceRunID = "aws-generation-" + serviceKind + "-1"
	item.GenerationID = item.SourceRunID
	return item
}
