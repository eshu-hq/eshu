package sandbox

import (
	"strings"
	"testing"
)

// TestNormalizeNoPanic confirms that hostile inputs never cause a panic regardless of content.
// Returning an error (e.g. "control character not permitted") is an acceptable non-panic result.
func TestNormalizeNoPanic(t *testing.T) {
	t.Parallel()

	hostile := []string{
		"",
		"   ",
		"\x00",
		"\x00\x01\x02\x03",
		"SELECT '\x00",
		"SELECT /*\x00",
		"'",
		"/*",
		"$$",
		"$tag$",
		"``",
		`"`,
		strings.Repeat("'", 10000),
		strings.Repeat("/*", 5000),
		strings.Repeat("$", 5000),
		"SELECT '" + strings.Repeat("a", 65536) + "'",
		"\xff\xfe",                      // invalid UTF-8
		"SELECT 1\x00; DROP TABLE t",    // NUL in code
		"'a''b''c''d" + "'",             // many doubled quotes, then terminated
		"$tag1$body$tag2$mismatch here", // tag mismatch — unterminated
	}

	for _, q := range hostile {
		q := q
		for _, d := range []Dialect{DialectSQL, DialectCypher} {
			d := d
			t.Run(string(d)+"/"+truncateLabel(q), func(t *testing.T) {
				t.Parallel()
				// The only guarantee is no panic.
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("normalize panicked: %v", r)
					}
				}()
				_ = normalize(d, q)
			})
		}
	}
}

// truncateLabel shortens a string to a safe test-name length.
func truncateLabel(s string) string {
	const maxLen = 32
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
