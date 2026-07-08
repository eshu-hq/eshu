// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

func decodeAWSIAMPermission(env facts.Envelope) (iamv1.Permission, error) {
	permission, err := factschema.DecodeAWSIAMPermission(factschemaEnvelope(env))
	if err != nil {
		return iamv1.Permission{}, newProjectorDecodeError(factschema.FactKindAWSIAMPermission, err)
	}
	return permission, nil
}
