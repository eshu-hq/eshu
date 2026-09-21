// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"errors"
	"strings"

	awsauditmanagertypes "github.com/aws/aws-sdk-go-v2/service/auditmanager/types"
	"github.com/aws/smithy-go"
)

// isThrottleError reports whether err is an AWS throttle response so the shared
// telemetry can record it on the throttle counter without the adapter retrying.
func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException"
}

// isAccessDenied reports whether err is an Audit Manager AccessDeniedException.
// A single denied tag read degrades to summary-only metadata; a denied
// account-status read is treated as an unregistered account rather than a scan
// failure.
func isAccessDenied(err error) bool {
	var apiErr *awsauditmanagertypes.AccessDeniedException
	return errors.As(err, &apiErr)
}

// isResourceNotFound reports whether err is an Audit Manager
// ResourceNotFoundException, returned both for a removed resource and for an
// account that has not enabled Audit Manager.
func isResourceNotFound(err error) bool {
	var apiErr *awsauditmanagertypes.ResourceNotFoundException
	return errors.As(err, &apiErr)
}

// isNotRegistered reports whether err is Audit Manager's account-scoped "not
// enabled" response. Audit Manager returns AccessDeniedException or
// ResourceNotFoundException when the account has not activated Audit Manager.
// Treating that single account-status error as an empty scan keeps an
// unregistered account from failing the whole claim while still surfacing
// genuine authorization or missing-resource failures elsewhere.
func isNotRegistered(err error) bool {
	return isAccessDenied(err) || isResourceNotFound(err)
}

// errorClass returns the AWS error code for warning telemetry, or a generic
// "error" label when err is not an AWS API error.
func errorClass(err error) string {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode()
	}
	return "error"
}
