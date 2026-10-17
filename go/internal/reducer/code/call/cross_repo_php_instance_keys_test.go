// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import "testing"

// The #8280 PHP instance keys end to end: `$logger->info()` on a variable
// written once by `new Logger()` resolves to the producer's public instance
// method only when exactly one repository defines the key.

const phpInstanceKeyProducerComposer = `{"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}}}`

func phpInstanceKeyProducer(t *testing.T, repoID string) packageKeyRepo {
	t.Helper()
	return parsePackageKeyRepo(t, repoID, map[string]string{
		"composer.json":  phpInstanceKeyProducerComposer,
		"src/Logger.php": "<?php\nnamespace Acme\\Logging;\n\nclass Logger\n{\n    public function info(string $message): void {}\n    private function secret(): void {}\n}\n",
	})
}

func phpInstanceKeyConsumer(t *testing.T) packageKeyRepo {
	t.Helper()
	return parsePackageKeyRepo(t, "repo-app", map[string]string{
		"composer.json": `{"name": "acme/app", "require": {"acme/logging": "^1.0"}, "autoload": {"psr-4": {"App\\": "app/"}}}`,
		"composer.lock": `{"packages": [{"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}}}]}`,
		"app/Service.php": "<?php\nnamespace App;\n\nuse Acme\\Logging\\Logger;\n\nclass Service\n{\n    public function run(): void\n    {\n" +
			"        $logger = new Logger();\n        $logger->info('x');\n    }\n}\n",
	})
}

func TestExtractRowsResolvesParsedPHPInstanceKeysAcrossRepositories(t *testing.T) {
	t.Parallel()

	consumer := phpInstanceKeyConsumer(t)
	producer := phpInstanceKeyProducer(t, "repo-logging")
	assertReferencedKeys(t, consumer, []string{`package:acme/logging#Acme\Logging\Logger->info`})

	_, rows := ExtractRows(append(consumer.envelopes, producer.envelopes...))
	assertImportBindingRows(t, callRowsFrom(rows, "uid:repo-app:run"), "run", []string{"uid:repo-logging:info"})
}

func TestExtractRowsLeavesParsedPHPInstanceKeysUnresolvedWhenTwoProducersShareTheName(t *testing.T) {
	t.Parallel()

	consumer := phpInstanceKeyConsumer(t)
	first := phpInstanceKeyProducer(t, "repo-logging")
	mirror := phpInstanceKeyProducer(t, "repo-logging-mirror")

	_, rows := ExtractRows(append(append(consumer.envelopes, first.envelopes...), mirror.envelopes...))
	assertNoRowsTo(t, rows, "uid:repo-app:run", []string{"uid:repo-logging:info", "uid:repo-logging-mirror:info"})
}
