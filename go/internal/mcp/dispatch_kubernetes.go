// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
	kubernetestools "github.com/eshu-hq/eshu/go/internal/mcp/kubernetes"
)

// kubernetesCorrelationsRoute adapts the child package's Kubernetes-correlation
// request selection into the root dispatcher's transport route.
func kubernetesCorrelationsRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(kubernetestools.Route(toolName, routecontract.Arguments(args)))
}
