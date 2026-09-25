// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind

import (
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	svc "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/route53recoverycontrolconfig"
	sdkadapter "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/route53recoverycontrolconfig/sdk"
)

func init() {
	runtime.Register(runtime.ScannerRegistration{
		ServiceKind: aws.ServiceRoute53RecoveryControlConfig,
		Build: func(d runtime.ScannerDeps) (runtime.ServiceScanner, error) {
			return svc.Scanner{
				Client: sdkadapter.NewClient(d.AWSConfig, d.Boundary, d.Tracer, d.Instruments),
			}, nil
		},
	})
}
