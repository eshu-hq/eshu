// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	kubernetestools "github.com/eshu-hq/eshu/go/internal/mcp/kubernetes"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// kubernetesCorrelationsRoute adapts the child package's Kubernetes-correlation
// request selection into the root dispatcher's transport route.
func kubernetesCorrelationsRoute(toolName string, args map[string]any) (*route, bool) {
	request, handled := kubernetestools.Route(toolName, routecontract.Arguments(args))
	if !handled {
		return nil, false
	}
	return &route{
		method: request.Method,
		path:   request.Path,
		body:   request.Body,
		query:  request.Query,
	}, true
}
