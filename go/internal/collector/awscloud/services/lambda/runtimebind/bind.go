// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind binds the Lambda service scanner into the
// awsruntime registry. Importing this package for its init side effect adds
// the production scanner to the registry without modifying any shared file.
package runtimebind

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	svc "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/lambda"
	sdkadapter "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/lambda/awssdk"
)

func init() {
	awsruntime.Register(awsruntime.ScannerRegistration{
		ServiceKind:          awscloud.ServiceLambda,
		RequiresRedactionKey: true,
		Build: func(d awsruntime.ScannerDeps) (awsruntime.ServiceScanner, error) {
			if d.RedactionKey.IsZero() {
				return nil, fmt.Errorf("lambda scanner redaction key is required")
			}
			return svc.Scanner{
				Client:       sdkadapter.NewClient(d.AWSConfig, d.Boundary, d.Tracer, d.Instruments),
				RedactionKey: d.RedactionKey,
			}, nil
		},
	})
}
