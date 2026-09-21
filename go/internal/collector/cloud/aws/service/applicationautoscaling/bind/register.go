// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind

import (
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	svc "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/applicationautoscaling"
	sdkadapter "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/applicationautoscaling/sdk"
)

func init() {
	runtime.Register(runtime.ScannerRegistration{
		ServiceKind: aws.ServiceApplicationAutoScaling,
		Build: func(d runtime.ScannerDeps) (runtime.ServiceScanner, error) {
			return svc.Scanner{
				Client: sdkadapter.NewClient(d.AWSConfig, d.Boundary, d.Tracer, d.Instruments),
			}, nil
		},
	})
}
