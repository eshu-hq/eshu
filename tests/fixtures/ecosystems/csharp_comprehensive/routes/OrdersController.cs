// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// #5361 route query-proof matrix: cribs the ASP.NET attribute-route
// controller shape proven by internal/parser/csharp_route_semantics_test.go
// (TestDefaultEngineParsePathCSharpASPNetAttributeRouteEntries).
using Microsoft.AspNetCore.Mvc;

[ApiController]
[Route("api/orders")]
public sealed class OrdersController : ControllerBase {
    [HttpGet("{id}")]
    public string Get(string id) => id;
}
