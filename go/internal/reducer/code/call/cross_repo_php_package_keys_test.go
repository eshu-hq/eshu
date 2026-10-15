// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// The #7601 cross-repository PHP join, driven end to end from real parser
// output: the producer's package_id/export_name come from its composer.json
// name and psr-4 map, and the consumer's package_export_symbol from its
// composer.lock. These tests fail if either side's keys drift apart.

const phpPackageKeyProducerComposer = `{"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}}}`

const phpPackageKeyProducerLogger = `<?php
namespace Acme\Logging;

class Logger
{
    public static function create(): self
    {
        return new self();
    }
}
`

const phpPackageKeyConsumerLock = `{"packages": [{"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}}}]}`

const phpPackageKeyConsumerService = `<?php
namespace App;

use Acme\Logging\Logger;

class Service
{
    public function run(): void
    {
        $logger = new Logger();
        $factory = Logger::create();
    }
}
`

func phpPackageKeyConsumer(t *testing.T) packageKeyRepo {
	t.Helper()
	return parsePackageKeyRepo(t, "repo-app", map[string]string{
		"composer.json":   `{"name": "acme/app", "require": {"acme/logging": "^1.0"}, "autoload": {"psr-4": {"App\\": "app/"}}}`,
		"composer.lock":   phpPackageKeyConsumerLock,
		"app/Service.php": phpPackageKeyConsumerService,
	})
}

func phpPackageKeyProducer(t *testing.T, repoID string) packageKeyRepo {
	t.Helper()
	return parsePackageKeyRepo(t, repoID, map[string]string{
		"composer.json":  phpPackageKeyProducerComposer,
		"src/Logger.php": phpPackageKeyProducerLogger,
	})
}

func TestExtractRowsResolvesParsedPHPComposerImportAcrossRepositories(t *testing.T) {
	t.Parallel()

	consumer := phpPackageKeyConsumer(t)
	producer := phpPackageKeyProducer(t, "repo-logging")

	_, rows := ExtractRows(append(consumer.envelopes, producer.envelopes...))
	fromRun := callRowsFrom(rows, "uid:repo-app:run")

	// new Logger() resolves to the class, Logger::create() to the method.
	for _, callee := range []string{"uid:repo-logging:Logger", "uid:repo-logging:create"} {
		row, ok := fromRun[callee]
		if !ok {
			t.Fatalf("no CALLS row run -> %s; rows from run = %#v", callee, fromRun)
		}
		if got := payloadcore.AnyToString(row["resolution_method"]); got != string(codeprovenance.MethodImportBinding) {
			t.Errorf("%s resolution_method = %q, want %q", callee, got, codeprovenance.MethodImportBinding)
		}
		if got := payloadcore.AnyToString(row["repo_id"]); got != "repo-app" {
			t.Errorf("%s repo_id = %q, want repo-app", callee, got)
		}
	}
}

// TestExtractRowsKeepsLaravelStyleOwnClassOutOfAnotherRepository pins the
// Laravel-style negative: two applications both declare App\ and define
// App\Models\User. The consumer's own App\ class is not in its composer.lock,
// so its call carries no key and never resolves into the other application.
func TestExtractRowsKeepsLaravelStyleOwnClassOutOfAnotherRepository(t *testing.T) {
	t.Parallel()

	userModel := "<?php\nnamespace App\\Models;\n\nclass User\n{\n    public static function find(int $id): ?self\n    {\n        return null;\n    }\n}\n"
	other := parsePackageKeyRepo(t, "repo-other-app", map[string]string{
		"composer.json":       `{"name": "acme/other-app", "autoload": {"psr-4": {"App\\": "app/"}}}`,
		"app/Models/User.php": userModel,
	})
	consumer := parsePackageKeyRepo(t, "repo-app", map[string]string{
		"composer.json":           `{"name": "acme/app", "autoload": {"psr-4": {"App\\": "app/"}}}`,
		"composer.lock":           phpPackageKeyConsumerLock,
		"app/Http/Controller.php": "<?php\nnamespace App\\Http;\n\nuse App\\Models\\User;\n\nclass Controller\n{\n    public function show(): void\n    {\n        User::find(1);\n        new User();\n    }\n}\n",
	})

	// A locked dev tool that declares the same App\ prefix (laravel/pint does)
	// and defines the same class must not capture the application's own call.
	pint := parsePackageKeyRepo(t, "repo-pint", map[string]string{
		"composer.json":       `{"name": "laravel/pint", "autoload": {"psr-4": {"App\\": "app/"}}}`,
		"app/Models/User.php": userModel,
	})
	pintConsumer := parsePackageKeyRepo(t, "repo-app-with-pint", map[string]string{
		"composer.json":           `{"name": "acme/app", "autoload": {"psr-4": {"App\\": "app/"}}}`,
		"composer.lock":           `{"packages-dev": [{"name": "laravel/pint", "autoload": {"psr-4": {"App\\": "app/"}}}]}`,
		"app/Http/Controller.php": "<?php\nnamespace App\\Http;\n\nuse App\\Models\\User;\n\nclass Controller\n{\n    public function index(): void\n    {\n        User::find(1);\n        new User();\n    }\n}\n",
	})

	envelopes := append(consumer.envelopes, other.envelopes...)
	envelopes = append(envelopes, pint.envelopes...)
	_, rows := ExtractRows(append(envelopes, pintConsumer.envelopes...))
	for _, caller := range []string{"uid:repo-app:show", "uid:repo-app-with-pint:index"} {
		for callee := range callRowsFrom(rows, caller) {
			if strings.HasPrefix(callee, "uid:repo-other-app:") || strings.HasPrefix(callee, "uid:repo-pint:") {
				t.Fatalf("%s resolved into another repository: %s", caller, callee)
			}
		}
	}
}

// TestExtractRowsLeavesParsedPHPComposerImportUnresolvedWhenTwoProducersShareTheName
// pins unique-or-unresolved: a split mirror (a monorepo and its subtree split
// both stamp acme/logging) gives two definitions per key, so no edge.
func TestExtractRowsLeavesParsedPHPComposerImportUnresolvedWhenTwoProducersShareTheName(t *testing.T) {
	t.Parallel()

	consumer := phpPackageKeyConsumer(t)
	first := phpPackageKeyProducer(t, "repo-logging")
	mirror := phpPackageKeyProducer(t, "repo-logging-mirror")

	envelopes := append(consumer.envelopes, first.envelopes...)
	_, rows := ExtractRows(append(envelopes, mirror.envelopes...))
	for callee := range callRowsFrom(rows, "uid:repo-app:run") {
		for _, forbidden := range []string{
			"uid:repo-logging:Logger", "uid:repo-logging:create",
			"uid:repo-logging-mirror:Logger", "uid:repo-logging-mirror:create",
		} {
			if callee == forbidden {
				t.Fatalf("run resolved to %s although two repositories define the key", callee)
			}
		}
	}
}
