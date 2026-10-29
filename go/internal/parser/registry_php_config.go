// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"strings"
)

// phpConfigDefinition binds only known PHP configuration names to guarded,
// metadata-only structured parsing.
func phpConfigDefinition() Definition {
	return Definition{
		ParserKey: "php_config", Language: "php_config", Ecosystem: "php",
		Admission:    AdmissionStructuredFact,
		ContentStore: ContentStore{Policy: ContentStoreMetadata},
		ExactNames:   []string{"php.ini", ".htaccess", "httpd.conf", "apache2.conf"},
	}
}

func phpConfigPathDispatch() PathDispatch {
	return PathDispatch{
		Patterns:  []string{"conf.d/*.ini", "php-fpm.d/*.conf", "apache/**/*.conf", "apache2/**/*.conf", "httpd/**/*.conf"},
		ParserKey: "php_config",
		Examples:  []string{"conf.d/site.ini", "php-fpm.d/www.conf", "apache2/sites-enabled/site.conf"},
	}
}

// isPHPConfigPath limits metadata-only admission to PHP and Apache locations.
func isPHPConfigPath(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	base := filepath.Base(normalized)
	switch {
	case strings.HasSuffix(base, ".ini"):
		return strings.Contains(normalized, "/conf.d/") || strings.HasPrefix(normalized, "conf.d/")
	case strings.HasSuffix(base, ".conf"):
		return strings.Contains(normalized, "/php-fpm.d/") || strings.HasPrefix(normalized, "php-fpm.d/") ||
			strings.Contains(normalized, "/apache/") || strings.HasPrefix(normalized, "apache/") ||
			strings.Contains(normalized, "/apache2/") || strings.HasPrefix(normalized, "apache2/") ||
			strings.Contains(normalized, "/httpd/") || strings.HasPrefix(normalized, "httpd/")
	default:
		return false
	}
}
