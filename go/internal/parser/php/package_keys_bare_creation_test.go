// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// PHP 8.4 allows a member call on an unparenthesized `new X()`. The pinned
// tree-sitter-php grammar parses it as a member_call_expression whose object is
// a bare object_creation_expression, for any target PHP version, so it keys by
// the same class rules as `(new X())->m(`.

const phpBareCreationConsumer = `<?php
namespace App;

use Acme\Logging\Logger;
use Acme\Logging\Logger as AliasLogger;
use Vendor\Other;

class Service
{
    public function run(string $cls, array $args): void
    {
        new Logger()->info('bare');
        new AliasLogger()->info('bare aliased');
        new \Acme\Logging\Logger()->info('bare literal');
        new Logger(...$args)->info('bare spread');
        (new Logger)->info('parenthesized no args');
        new Logger()->info('chained head')->debug('chained tail');
        new $cls()->info('dynamic class');
        new class {}->info('anonymous class');
        new Other()->info('unlocked package');
        new Local()->info('own namespace');
        new Logger()?->info('nullsafe');
    }
}
`

func TestDefaultEngineParsePathPHPKeysUnparenthesizedCreationReceivers(t *testing.T) {
	t.Parallel()

	got := parsePHPProducerRepo(t, map[string]string{
		"composer.json":   phpConsumerComposerJSON,
		"composer.lock":   phpMemberConsumerLock,
		"app/Service.php": phpBareCreationConsumer,
	}, "app/Service.php")
	const logger = `package:acme/logging#Acme\Logging\Logger->info`
	for _, marker := range []string{"'bare'", "'bare aliased'", "'bare literal'", "'bare spread'", "'parenthesized no args'", "'chained head'"} {
		call := phpCallByNameOnLine(t, got, phpBareCreationConsumer, marker, "info")
		parsertest.AssertStringFieldValue(t, call, "package_export_symbol", logger)
	}
	for _, tc := range []struct {
		why    string
		marker string
		name   string
	}{
		{"the chain's next receiver is a call result", "'chained tail'", "debug"},
		{"a dynamic class name", "'dynamic class'", "info"},
		{"an anonymous class", "'anonymous class'", "info"},
		{"a class no lock entry covers", "'unlocked package'", "info"},
		{"the consumer's own class", "'own namespace'", "info"},
		{"a nullsafe call", "'nullsafe'", "info"},
	} {
		call := phpCallByNameOnLine(t, got, phpBareCreationConsumer, tc.marker, tc.name)
		if value, ok := call["package_export_symbol"]; ok {
			t.Errorf("%s: package_export_symbol = %#v, want absent", tc.why, value)
		}
	}
}
