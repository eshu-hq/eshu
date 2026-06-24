// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

type terraformBackendFactContext struct {
	Backends  []map[string]any `json:"terraform_backends"`
	Variables []map[string]any `json:"terraform_variables"`
	Locals    []map[string]any `json:"terraform_locals"`
}

func terraformBackendCandidatesFromContext(
	repoID string,
	contextValue terraformBackendFactContext,
) []terraformstate.DiscoveryCandidate {
	return terraformstate.EvaluateBackendConfig(
		repoID,
		terraformstate.BackendConfigContext{
			Backends:  contextValue.Backends,
			Variables: contextValue.Variables,
			Locals:    contextValue.Locals,
		},
	).Candidates
}

func mergeTerraformBackendFactContext(
	dst terraformBackendFactContext,
	src terraformBackendFactContext,
) terraformBackendFactContext {
	dst.Backends = append(dst.Backends, src.Backends...)
	dst.Variables = append(dst.Variables, src.Variables...)
	dst.Locals = append(dst.Locals, src.Locals...)
	return dst
}

func decodeTerraformBackendFactContext(rawContext []byte) (terraformBackendFactContext, error) {
	trimmed := strings.TrimSpace(string(rawContext))
	if trimmed == "" {
		return terraformBackendFactContext{}, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var backends []map[string]any
		if err := json.Unmarshal(rawContext, &backends); err != nil {
			return terraformBackendFactContext{}, err
		}
		return terraformBackendFactContext{Backends: backends}, nil
	}

	var contextValue terraformBackendFactContext
	if err := json.Unmarshal(rawContext, &contextValue); err != nil {
		return terraformBackendFactContext{}, err
	}
	return contextValue, nil
}
