// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	containerimagetools "github.com/eshu-hq/eshu/go/internal/mcp/container/image"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// containerImageRoute adapts the child package's container-image identity
// request into the root dispatcher's transport route.
func containerImageRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	return adaptChildRoute(containerimagetools.Route(toolName, routecontract.Arguments(args)))
}
