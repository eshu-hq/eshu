// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPHPEmitsSymfonyRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "ReportController.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php
namespace App\Http\Controllers;

use Symfony\Component\Routing\Attribute\Route;

final class ReportController {
    #[Route('/reports/{id}', methods: ['GET'], name: 'reports_show')]
    public function show(): string {
        return 'show';
    }

    #[Route(path: '/reports', methods: ['POST'])]
    public function create(): string {
        return 'create';
    }

    #[Route('/reports/{id}/preview')]
    public function preview(): string {
        return 'preview';
    }

    #[Route($dynamicPath, methods: ['DELETE'])]
    public function dynamicPath(): string {
        return 'dynamic';
    }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertFrameworksEqual(t, got, "symfony")
	assertNestedRouteEntriesEqual(t, got, "symfony", []map[string]string{
		{"method": "GET", "path": "/reports/{id}", "handler": "ReportController.show"},
		{"method": "POST", "path": "/reports", "handler": "ReportController.create"},
		{"method": "ANY", "path": "/reports/{id}/preview", "handler": "ReportController.preview"},
	})
}

func TestDefaultEngineParsePathPHPSkipsNonExactSymfonyRoutes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "DynamicController.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php
namespace App\Http\Controllers;

use Symfony\Component\Routing\Attribute\Route;

final class DynamicController {
    #[Route(self::DYNAMIC_PATH, methods: ['GET'])]
    public function dynamicPath(): string {
        return 'dynamic';
    }

    #[Route('/dynamic-method', methods: [self::METHOD])]
    public function dynamicMethod(): string {
        return 'dynamic';
    }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	semantics, ok := got["framework_semantics"].(map[string]any)
	if !ok {
		return
	}
	nested, ok := semantics["symfony"].(map[string]any)
	if !ok {
		return
	}
	if _, ok := nested["route_entries"]; ok {
		t.Fatalf("framework_semantics.symfony.route_entries = %#v, want absent for non-exact Symfony routes", nested["route_entries"])
	}
}

func TestDefaultEngineParsePathPHPSkipsUnresolvedBareRouteAttribute(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "CustomController.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php
namespace App\Http\Controllers;

final class CustomController {
    #[Route('/custom', methods: ['GET'])]
    public function custom(): string {
        return 'custom';
    }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	if semantics, ok := got["framework_semantics"]; ok {
		t.Fatalf("framework_semantics = %#v, want absent for an unresolved bare Route attribute", semantics)
	}
}

func TestDefaultEngineParsePathPHPEmitsSlimRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "routes.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php

use Slim\Factory\AppFactory;

$app = AppFactory::create();

$app->get('/', function ($req, $res) { return $res; });
$app->post('/users', \App\Action\CreateUserAction::class);
$app->map(['GET', 'POST'], '/multi', 'Handler:method');
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertFrameworksEqual(t, got, "slim")
	assertNestedRouteEntriesEqual(t, got, "slim", []map[string]string{
		{"method": "GET", "path": "/", "handler": ""},
		{"method": "POST", "path": "/users", "handler": "CreateUserAction"},
		{"method": "GET", "path": "/multi", "handler": "Handler:method"},
		{"method": "POST", "path": "/multi", "handler": "Handler:method"},
	})
}

func TestDefaultEngineParsePathPHPSkipsNonSlimGetCall(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "not_slim.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php

$collection = new SomeCollection();
$item = $collection->get($id);
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	semantics, ok := got["framework_semantics"].(map[string]any)
	if !ok {
		return
	}
	if _, ok := semantics["slim"]; ok {
		t.Fatalf("framework_semantics.slim should be absent for non-Slim get() call")
	}
}

func TestDefaultEngineParsePathPHPEmitsSlimGroupedAndNestedRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "grouped_routes.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php

use Slim\App;
use Slim\Interfaces\RouteCollectorProxyInterface;

	return function (App $app) {
    $app->get('/', \App\Action\HomeAction::class);
    $app->group('/users', function (RouteCollectorProxyInterface $group) {
        $group->get('', \App\Action\ListUsers::class);
        $group->get('/{id}', \App\Action\ViewUser::class);
        $group->group('/{id}/posts', function (RouteCollectorProxyInterface $g) {
            $g->get('', \App\Action\ListPosts::class);
        });
    });
};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertFrameworksEqual(t, got, "slim")
	assertNestedRouteEntriesEqual(t, got, "slim", []map[string]string{
		{"method": "GET", "path": "/", "handler": "HomeAction"},
		{"method": "GET", "path": "/users", "handler": "ListUsers"},
		{"method": "GET", "path": "/users/{id}", "handler": "ViewUser"},
		{"method": "GET", "path": "/users/{id}/posts", "handler": "ListPosts"},
	})
}

func TestDefaultEngineParsePathPHPSkipsSlimRouteWithEmptyPath(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "empty_path.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php

use Slim\Factory\AppFactory;

$app = AppFactory::create();
$app->get('', 'SomeHandler::class');
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	// Empty-path route must not be emitted — an empty path is wrong truth.
	semantics, ok := got["framework_semantics"].(map[string]any)
	if !ok {
		return
	}
	if _, ok := semantics["slim"]; ok {
		t.Fatalf("framework_semantics.slim should be absent when the only route has an empty path")
	}
}

func TestDefaultEngineParsePathPHPSkipsNonSlimReceiverGetCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "container.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php

use Slim\Factory\AppFactory;

$app = AppFactory::create();
$container = new \Some\Psr\Container();
$cache = new \Some\Cache\Pool();

// These are NOT Slim routes — receivers have no Slim provenance.
$container->get('settings');
$cache->get('user:1');
$cache->delete('stale-key');
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	// $app->get(...) is absent from this fixture, so slim should not
	// appear at all — the container/cache calls must not be emitted.
	semantics, ok := got["framework_semantics"].(map[string]any)
	if !ok {
		return
	}
	if _, ok := semantics["slim"]; ok {
		t.Fatalf("framework_semantics.slim should be absent: container->get('settings') and cache->get('user:1') have non-Slim receivers")
	}
}

func TestDefaultEngineParsePathPHPEmitsLaravelRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "routes.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php

use Illuminate\Support\Facades\Route;

Route::post('users/login', 'AuthController@login');
Route::get('user', 'UserController@index');
Route::delete('users/{id}', 'UserController@destroy');
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertFrameworksEqual(t, got, "laravel")
	assertNestedRouteEntriesEqual(t, got, "laravel", []map[string]string{
		{"method": "POST", "path": "users/login", "handler": "AuthController@login"},
		{"method": "GET", "path": "user", "handler": "UserController@index"},
		{"method": "DELETE", "path": "users/{id}", "handler": "UserController@destroy"},
	})
}

func TestDefaultEngineParsePathPHPSkipsNonLaravelScopedGetCall(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "not_laravel.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php

$result = SomeClass::get('config-key');
$other = \App\Utils\Helper::post('/some/path');
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	semantics, ok := got["framework_semantics"].(map[string]any)
	if !ok {
		return
	}
	if _, ok := semantics["laravel"]; ok {
		t.Fatalf("framework_semantics.laravel should be absent for non-Laravel scoped calls")
	}
}

func TestDefaultEngineParsePathPHPEmitsLaravelNestedGroupRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "nested_group.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php

use Illuminate\Support\Facades\Route;

Route::group(['prefix' => 'api'], function () {
    Route::group(['prefix' => 'v1'], function () {
        Route::get('users', 'UserController@index');
        Route::post('users', 'UserController@store');
    });
});
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertFrameworksEqual(t, got, "laravel")
	assertNestedRouteEntriesEqual(t, got, "laravel", []map[string]string{
		{"method": "GET", "path": "api/v1/users", "handler": "UserController@index"},
		{"method": "POST", "path": "api/v1/users", "handler": "UserController@store"},
	})
}

func TestDefaultEngineParsePathPHPEmitsLaravelGlobalBackslashRouteInNamespace(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "namespaced.php")
	writeTestFile(
		t,
		sourcePath,
		`<?php

namespace App\Http;

// No "use Illuminate\Support\Facades\Route;" import here.
// Explicit \Route:: must be the global alias and still resolve.
\Route::get('users', 'UserController@index');

// Bare Route:: in a namespaced file without a Route import resolves
// to App\Http\Route, which is NOT the Laravel facade — must NOT emit.
Route::get('profiles', 'ProfileController@show');
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertFrameworksEqual(t, got, "laravel")
	assertNestedRouteEntriesEqual(t, got, "laravel", []map[string]string{
		// \Route::get(...) — global alias, must emit.
		{"method": "GET", "path": "users", "handler": "UserController@index"},
		// Bare Route::get(...) in namespaced file without import — must NOT emit.
	})
}
