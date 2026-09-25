// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	svc "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/codebuild"
	sdkadapter "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/codebuild/sdk"
)

func init() {
	runtime.Register(runtime.ScannerRegistration{
		ServiceKind:          aws.ServiceCodeBuild,
		RequiresRedactionKey: true,
		Build: func(d runtime.ScannerDeps) (runtime.ServiceScanner, error) {
			if d.RedactionKey.IsZero() {
				return nil, fmt.Errorf("codebuild scanner redaction key is required")
			}
			return svc.Scanner{
				Client:       sdkadapter.NewClient(d.AWSConfig, d.Boundary, d.Tracer, d.Instruments, d.RedactionKey),
				RedactionKey: d.RedactionKey,
			}, nil
		},
	})
}
