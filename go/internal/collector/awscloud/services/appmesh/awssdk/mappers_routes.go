// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	appmeshtypes "github.com/aws/aws-sdk-go-v2/service/appmesh/types"

	appmeshservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/appmesh"
)

func mapRoute(meshName, routerName, routerARN, name string, detail *appmeshtypes.RouteData) appmeshservice.Route {
	route := appmeshservice.Route{
		Name:              name,
		MeshName:          meshName,
		VirtualRouterName: routerName,
		VirtualRouterARN:  strings.TrimSpace(routerARN),
	}
	if detail == nil {
		return route
	}
	if metadata := detail.Metadata; metadata != nil {
		route.ARN = strings.TrimSpace(aws.ToString(metadata.Arn))
		route.CreatedAt = timeOrZero(metadata.CreatedAt)
		route.LastUpdatedAt = timeOrZero(metadata.LastUpdatedAt)
	}
	if detail.Spec != nil {
		route.Priority = detail.Spec.Priority
		applyRouteSpec(&route, detail.Spec)
	}
	if detail.Status != nil {
		route.Status = string(detail.Status.Status)
	}
	return route
}

// applyRouteSpec records the route protocol kind and, for HTTP/HTTP2 routes,
// the match shape (path prefix/exact, method, header matches). gRPC and TCP
// routes record the protocol kind only. Header match VALUES are passed through
// verbatim; the scanner owns redaction because it holds the redaction key.
func applyRouteSpec(route *appmeshservice.Route, spec *appmeshtypes.RouteSpec) {
	switch {
	case spec.HttpRoute != nil:
		route.ProtocolKind = "http"
		applyHTTPMatch(route, spec.HttpRoute.Match)
	case spec.Http2Route != nil:
		route.ProtocolKind = "http2"
		applyHTTPMatch(route, spec.Http2Route.Match)
	case spec.GrpcRoute != nil:
		route.ProtocolKind = "grpc"
	case spec.TcpRoute != nil:
		route.ProtocolKind = "tcp"
	}
}

func applyHTTPMatch(route *appmeshservice.Route, match *appmeshtypes.HttpRouteMatch) {
	if match == nil {
		return
	}
	route.Method = string(match.Method)
	route.PathPrefix = strings.TrimSpace(aws.ToString(match.Prefix))
	if match.Path != nil {
		route.PathExact = strings.TrimSpace(aws.ToString(match.Path.Exact))
	}
	for _, header := range match.Headers {
		matchType, value := headerMatchTypeAndValue(header.Match)
		route.HeaderMatches = append(route.HeaderMatches, appmeshservice.HeaderMatch{
			Name:      strings.TrimSpace(aws.ToString(header.Name)),
			MatchType: matchType,
			Value:     value,
			Invert:    aws.ToBool(header.Invert),
		})
	}
}

// headerMatchTypeAndValue extracts the match operator and its literal value.
// Range matches carry numeric bounds, not a credential-shaped string, so they
// report the operator with no value.
func headerMatchTypeAndValue(method appmeshtypes.HeaderMatchMethod) (matchType, value string) {
	switch typed := method.(type) {
	case *appmeshtypes.HeaderMatchMethodMemberExact:
		return "exact", typed.Value
	case *appmeshtypes.HeaderMatchMethodMemberPrefix:
		return "prefix", typed.Value
	case *appmeshtypes.HeaderMatchMethodMemberSuffix:
		return "suffix", typed.Value
	case *appmeshtypes.HeaderMatchMethodMemberRegex:
		return "regex", typed.Value
	case *appmeshtypes.HeaderMatchMethodMemberRange:
		return "range", ""
	default:
		return "", ""
	}
}

func mapGatewayRoute(meshName, gatewayName, name string, detail *appmeshtypes.GatewayRouteData) appmeshservice.GatewayRoute {
	route := appmeshservice.GatewayRoute{
		Name:               name,
		MeshName:           meshName,
		VirtualGatewayName: gatewayName,
	}
	if detail == nil {
		return route
	}
	if metadata := detail.Metadata; metadata != nil {
		route.ARN = strings.TrimSpace(aws.ToString(metadata.Arn))
		route.VirtualGatewayARN = gatewayRouteParentARN(route.ARN)
		route.CreatedAt = timeOrZero(metadata.CreatedAt)
		route.LastUpdatedAt = timeOrZero(metadata.LastUpdatedAt)
	}
	if detail.Spec != nil {
		applyGatewayRouteSpec(&route, detail.Spec)
	}
	if detail.Status != nil {
		route.Status = string(detail.Status.Status)
	}
	return route
}

func applyGatewayRouteSpec(route *appmeshservice.GatewayRoute, spec *appmeshtypes.GatewayRouteSpec) {
	switch {
	case spec.HttpRoute != nil:
		route.ProtocolKind = "http"
		route.TargetVirtualServiceName = httpGatewayRouteTarget(spec.HttpRoute.Action)
	case spec.Http2Route != nil:
		route.ProtocolKind = "http2"
		route.TargetVirtualServiceName = httpGatewayRouteTarget(spec.Http2Route.Action)
	case spec.GrpcRoute != nil:
		route.ProtocolKind = "grpc"
		route.TargetVirtualServiceName = grpcGatewayRouteTarget(spec.GrpcRoute.Action)
	}
}

func httpGatewayRouteTarget(action *appmeshtypes.HttpGatewayRouteAction) string {
	if action == nil || action.Target == nil || action.Target.VirtualService == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(action.Target.VirtualService.VirtualServiceName))
}

func grpcGatewayRouteTarget(action *appmeshtypes.GrpcGatewayRouteAction) string {
	if action == nil || action.Target == nil || action.Target.VirtualService == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(action.Target.VirtualService.VirtualServiceName))
}

// gatewayRouteParentARN derives the parent virtual gateway ARN from a gateway
// route ARN by trimming the "/gatewayRoute/{name}" suffix. It keeps the
// partition, region, and account consistent with the value App Mesh reported
// rather than synthesizing a fresh ARN. It returns "" when the ARN does not
// carry the expected segment.
func gatewayRouteParentARN(gatewayRouteARN string) string {
	const segment = "/gatewayRoute/"
	index := strings.Index(gatewayRouteARN, segment)
	if index < 0 {
		return ""
	}
	return gatewayRouteARN[:index]
}
