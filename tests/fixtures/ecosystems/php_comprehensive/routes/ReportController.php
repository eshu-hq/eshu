<?php
// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// #5361 route query-proof matrix: cribs the Symfony attribute-route shape
// proven by internal/parser/php_route_entries_test.go
// (TestDefaultEngineParsePathPHPEmitsSymfonyRouteEntries). Unlike Laravel's
// facade convention (routes.php), Symfony's method-level #[Route] attribute
// binds a route directly to a bare method name, which does resolve through
// reducer.resolveHandlesRouteFunction's exact-name index.

namespace App\Http\Controllers;

use Symfony\Component\Routing\Attribute\Route;

final class ReportController
{
    #[Route('/reports/{id}', methods: ['GET'], name: 'reports_show')]
    public function show(): string
    {
        return 'show';
    }
}
