<?php
// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// #5361 route query-proof matrix: cribs the Laravel string-callable route
// shape proven by internal/parser/php_route_entries_test.go
// (TestDefaultEngineParsePathPHPEmitsLaravelRouteEntries).

use Illuminate\Support\Facades\Route;

Route::get('user', 'UserController@index');
Route::post('users/login', 'AuthController@login');
