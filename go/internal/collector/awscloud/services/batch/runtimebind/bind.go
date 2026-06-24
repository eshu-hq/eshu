// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	svc "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/batch"
	sdkadapter "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/batch/awssdk"
)

func init() {
	awsruntime.Register(awsruntime.ScannerRegistration{
		ServiceKind:          awscloud.ServiceBatch,
		RequiresRedactionKey: true,
		Build: func(d awsruntime.ScannerDeps) (awsruntime.ServiceScanner, error) {
			if d.RedactionKey.IsZero() {
				return nil, fmt.Errorf("batch scanner redaction key is required")
			}
			return svc.Scanner{
				Client:       sdkadapter.NewClient(d.AWSConfig, d.Boundary, d.Tracer, d.Instruments),
				RedactionKey: d.RedactionKey,
			}, nil
		},
	})
}
