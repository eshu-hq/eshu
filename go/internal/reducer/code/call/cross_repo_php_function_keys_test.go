// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import "testing"

// The #8280 PHP function keys end to end: a bare `format_message()` bound by
// `use function` resolves to the producer's autoload.files function only when
// exactly one repository defines the key.

const phpFunctionKeyProducerComposer = `{
  "name": "acme/logging",
  "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}, "files": ["src/functions.php"]}
}`

func phpFunctionKeyProducer(t *testing.T, repoID string) packageKeyRepo {
	t.Helper()
	return parsePackageKeyRepo(t, repoID, map[string]string{
		"composer.json":     phpFunctionKeyProducerComposer,
		"src/functions.php": "<?php\nnamespace Acme\\Logging;\n\nfunction format_message(string $message): string { return $message; }\n",
	})
}

func phpFunctionKeyConsumer(t *testing.T) packageKeyRepo {
	t.Helper()
	return parsePackageKeyRepo(t, "repo-app", map[string]string{
		"composer.json": `{"name": "acme/app", "require": {"acme/logging": "^1.0"}, "autoload": {"psr-4": {"App\\": "app/"}}}`,
		"composer.lock": `{"packages": [{"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}, "files": ["src/functions.php"]}}]}`,
		"app/run.php": "<?php\nnamespace App;\n\nuse function Acme\\Logging\\format_message;\n\n" +
			"function run(): void\n{\n    format_message('y');\n}\n",
	})
}

func TestExtractRowsResolvesParsedPHPFunctionKeysAcrossRepositories(t *testing.T) {
	t.Parallel()

	consumer := phpFunctionKeyConsumer(t)
	producer := phpFunctionKeyProducer(t, "repo-logging")
	assertReferencedKeys(t, consumer, []string{`package:acme/logging#function:Acme\Logging\format_message`})

	_, rows := ExtractRows(append(consumer.envelopes, producer.envelopes...))
	assertImportBindingRows(t, callRowsFrom(rows, "uid:repo-app:run"), "run", []string{"uid:repo-logging:format_message"})
}

func TestExtractRowsLeavesParsedPHPFunctionKeysUnresolvedWhenTwoProducersShareTheName(t *testing.T) {
	t.Parallel()

	consumer := phpFunctionKeyConsumer(t)
	first := phpFunctionKeyProducer(t, "repo-logging")
	mirror := phpFunctionKeyProducer(t, "repo-logging-mirror")

	_, rows := ExtractRows(append(append(consumer.envelopes, first.envelopes...), mirror.envelopes...))
	assertNoRowsTo(t, rows, "uid:repo-app:run", []string{"uid:repo-logging:format_message", "uid:repo-logging-mirror:format_message"})
}
