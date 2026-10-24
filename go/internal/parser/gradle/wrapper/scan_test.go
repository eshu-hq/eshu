// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wrapper

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

const generatedWrapper = `distributionBase=GRADLE_USER_HOME
distributionPath=wrapper/dists
distributionUrl=https\://services.gradle.org/distributions/gradle-8.5-bin.zip
networkTimeout=10000
validateDistributionUrl=true
zipStoreBase=GRADLE_USER_HOME
zipStorePath=wrapper/dists
`

func TestScanGeneratedWrapperEmitsOneDistributionRow(t *testing.T) {
	t.Parallel()
	result := Scan([]byte(generatedWrapper))
	if result.State != StateParsed || len(result.Rows) != 1 {
		t.Fatalf("State=%q rows=%d, want parsed with one row", result.State, len(result.Rows))
	}
	row := result.Rows[0]
	want := map[string]any{
		"name": "gradle", "value": "8.5", "section": "distribution", "config_kind": "gradle_distribution",
		"lang": "gradle", "line_number": 3, "distribution_type": "bin",
	}
	for key, value := range want {
		if row[key] != value {
			t.Errorf("row[%q] = %v, want %v", key, row[key], value)
		}
	}
	if _, has := row["package_manager"]; has {
		t.Error("distribution row sets package_manager: it is not a package dependency")
	}
}

func TestScanVersionForms(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ url, version, kind string }{
		"all distribution":  {`https\://services.gradle.org/distributions/gradle-7.6.4-all.zip`, "7.6.4", "all"},
		"release candidate": {`https\://services.gradle.org/distributions/gradle-8.6-rc-1-bin.zip`, "8.6-rc-1", "bin"},
		"milestone":         {`https\://services.gradle.org/distributions/gradle-7.0-milestone-1-bin.zip`, "7.0-milestone-1", "bin"},
		"snapshot":          {`https\://services.gradle.org/distributions-snapshots/gradle-8.7-20240101000000+0000-bin.zip`, "8.7-20240101000000+0000", "bin"},
		"unescaped colon":   {`https://services.gradle.org/distributions/gradle-8.5-bin.zip`, "8.5", "bin"},
		"internal mirror":   {`https\://mirror.example.test/gradle/gradle-8.4-bin.zip`, "8.4", "bin"},
		"file url":          {`file\:///opt/dists/gradle-8.3-bin.zip`, "8.3", "bin"},
		"query string":      {`https\://mirror.example.test/dl/gradle-8.2-bin.zip?download=1`, "8.2", "bin"},
	}
	for name, tc := range cases {
		result := Scan([]byte("distributionUrl=" + tc.url + "\n"))
		if len(result.Rows) != 1 || result.Rows[0]["value"] != tc.version || result.Rows[0]["distribution_type"] != tc.kind {
			t.Errorf("%s: rows=%v, want version %q type %q", name, result.Rows, tc.version, tc.kind)
		}
	}
}

func TestScanSeparatorAndWhitespaceForms(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"colon separator":               "distributionUrl:https\\://h/gradle-8.5-bin.zip\n",
		"spaced equals":                 "distributionUrl = https\\://h/gradle-8.5-bin.zip\n",
		"space separator":               "distributionUrl https\\://h/gradle-8.5-bin.zip\n",
		"crlf":                          "distributionUrl=https\\://h/gradle-8.5-bin.zip\r\n",
		"leading blanks":                "   \tdistributionUrl=https\\://h/gradle-8.5-bin.zip\n",
		"continuation":                  "distributionUrl=https\\://h/\\\n    gradle-8.5-bin.zip\n",
		"no trailing eol":               "distributionUrl=https\\://h/gradle-8.5-bin.zip",
		"comment ending in a backslash": "# note \\\ndistributionUrl=https\\://h/gradle-8.5-bin.zip\n",
		"after a comment":               "# distributionUrl=https\\://h/gradle-9.9-bin.zip\n! another\ndistributionUrl=https\\://h/gradle-8.5-bin.zip\n",
		"escaped backslash":             "distributionUrl=https\\://h/dir\\\\x/gradle-8.5-bin.zip\n",
	} {
		result := Scan([]byte(text))
		if len(result.Rows) != 1 || result.Rows[0]["value"] != "8.5" {
			t.Errorf("%s: rows=%v, want one row with version 8.5", name, result.Rows)
		}
	}
}

func TestScanEmitsNothingWhenTheGrammarDoesNotMatch(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		text        string
		unsupported int
	}{
		"trailing blanks (kept by java)":  {"distributionUrl=https\\://h/gradle-8.5-bin.zip   \n", 1},
		"no distributionUrl":              {"distributionBase=GRADLE_USER_HOME\n", 0},
		"empty file":                      {"", 0},
		"key in another case":             {"DistributionUrl=https\\://h/gradle-8.5-bin.zip\n", 0},
		"commented out":                   {"#distributionUrl=https\\://h/gradle-8.5-bin.zip\n", 0},
		"key only in a longer key":        {"xdistributionUrl=https\\://h/gradle-8.5-bin.zip\n", 0},
		"not a gradle archive":            {"distributionUrl=https\\://h/dist/other-8.5-bin.zip\n", 1},
		"no version":                      {"distributionUrl=https\\://h/gradle--bin.zip\n", 1},
		"wrong extension":                 {"distributionUrl=https\\://h/gradle-8.5-bin.tar.gz\n", 1},
		"no distribution type":            {"distributionUrl=https\\://h/gradle-8.5.zip\n", 1},
		"version with a quote":            {"distributionUrl=https\\://h/gradle-8'5-bin.zip\n", 1},
		"unicode escape":                  {"distributionUrl=https\\://h/gradle-8.5\\u002d-bin.zip\n", 1},
		"unknown escape":                  {"distributionUrl=https\\://h/gradle-8.5\\q-bin.zip\n", 1},
		"version without a leading digit": {"distributionUrl=https\\://h/gradle-abc-bin.zip\n", 1},
		"key written with an escape":      {"distribution\\Url=https\\://h/gradle-8.5-bin.zip\n", 1},
		"empty value":                     {"distributionUrl=\n", 1},
		"repeated key":                    {"distributionUrl=https\\://h/gradle-8.5-bin.zip\ndistributionUrl=https\\://h/gradle-8.6-bin.zip\n", 1},
		"continuation into nothing":       {"distributionUrl=https\\://h/gradle-8.5-bin.zip\\\n", 1},
		"interpolation in the file":       {"distributionUrl=https\\://h/gradle-${v}-bin.zip\n", 1},
		"url over the field bound":        {"distributionUrl=https\\://h/" + strings.Repeat("d/", MaxValueBytes) + "gradle-8.5-bin.zip\n", 1},
		"version over the field bound":    {"distributionUrl=https\\://h/gradle-" + strings.Repeat("9", MaxVersionBytes+1) + "-bin.zip\n", 1},
	}
	for name, tc := range cases {
		result := Scan([]byte(tc.text))
		if result.State != StateParsed || len(result.Rows) != 0 || result.Counts.Unsupported != tc.unsupported {
			t.Errorf("%s: State=%q rows=%v Counts=%+v, want parsed, no rows, Unsupported=%d",
				name, result.State, result.Rows, result.Counts, tc.unsupported)
		}
	}
}

// TestParseNeverCarriesUserinfoOrAnyOtherValue is the credential contract: the
// URL userinfo, host, path and every other property value stay out of the
// payload, including when the caller asks for the source to be indexed.
func TestParseNeverCarriesUserinfoOrAnyOtherValue(t *testing.T) {
	t.Parallel()
	const secret = "s3cr3t-token-value"
	body := "distributionUrl=https\\://deploy:" + secret + "@mirror.example.test/dl/gradle-8.5-bin.zip\n" +
		"distributionSha256Sum=" + secret + "\nnetworkTimeout=" + secret + "\ncustomKey=" + secret + "\n"
	for _, indexSource := range []bool{false, true} {
		payload := parseText(t, body, indexSource)
		if rendered := fmt.Sprintf("%#v", payload); strings.Contains(rendered, secret) ||
			strings.Contains(rendered, "deploy") || strings.Contains(rendered, "mirror.example.test") {
			t.Fatalf("IndexSource=%v payload leaks wrapper content: %s", indexSource, rendered)
		}
		rows, _ := payload["variables"].([]map[string]any)
		if len(rows) != 1 || rows[0]["value"] != "8.5" {
			t.Fatalf("IndexSource=%v rows = %v, want the version still read", indexSource, rows)
		}
		if _, has := payload["source"]; has {
			t.Fatalf("IndexSource=%v payload carries source", indexSource)
		}
	}
}

func parseText(t *testing.T, text string, indexSource bool) map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gradle", "wrapper", "gradle-wrapper.properties")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := Parse(path, false, shared.Options{IndexSource: indexSource})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return payload
}

func TestScanReportsTheLineOfTheKeyAcrossContinuations(t *testing.T) {
	t.Parallel()
	text := "a=1\nlong=x\\\n  y\\\n  z\n\ndistributionUrl=https\\://h/gradle-8.5-bin.zip\n"
	result := Scan([]byte(text))
	if len(result.Rows) != 1 || result.Rows[0]["line_number"] != 6 {
		t.Fatalf("rows=%v, want one row at line 6", result.Rows)
	}
}
