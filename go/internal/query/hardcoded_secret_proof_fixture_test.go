// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
)

// secretProofFile is one content_files row the #7125 differential seeds through
// the real ContentWriter. An empty language stores NULL.
type secretProofFile struct {
	repo     string
	path     string
	language string
	body     string
}

// secretProofRequest names one argument set the differential runs through the
// legacy corpus scan and the side-table reader.
type secretProofRequest struct {
	name string
	req  codequery.HardcodedSecretInvestigationRequest
}

// secretProofHandFiles cover every classification and normalisation edge the
// ruling names: all six kinds, an sk_live line that matches the pattern but
// classifies as empty (the legacy query drops it), a short value that does not
// match, CRLF, lowercase akia, placeholder lines, all four suppressed path
// fragments, NULL and non-NULL language, and mixed-case or non-ASCII paths so a
// collation difference in ORDER BY would show.
func secretProofHandFiles() []secretProofFile {
	return []secretProofFile{
		{"repo-0", "src/kinds.go", "go", strings.Join([]string{
			"package kinds",
			`aws_access_key_id = AKIAABCDEFGHIJKLMNOP`,
			`key = "akiaabcdefghijklmnop"`,
			`-----BEGIN RSA PRIVATE KEY-----`,
			`slack: xoxb-1234567890-abcdef`,
			`  api_key: "abcdef123456"`,
			`PASSWORD=Hunter2xyz`,
			`client_secret = "s3cr3tvalue9"`,
			`Authorization: Bearer abcdefghijk`,
			`stripe = "sk_live_ABCDEFGH12"`,
			`token = "abc"`,
			`token = "abcdefgh" and AKIAABCDEFGHIJKLMNOP on one line`,
			`// refresh the token before the secret expires`,
			``,
		}, "\n")},
		{"repo-0", "src/crlf.py", "python", "import os\r\ntoken=abcdefgh1\r\npwd = 'hunter2hunter2'\r\nprint('ok')\r\n"},
		{"repo-0", "src/no_trailing_newline.go", "go", "x := 1\npassword := \"correcthorse\""},
		{"repo-0", "src/empty.go", "go", ""},
		{"repo-0", "src/clean.go", "go", "package clean\n\nfunc Add(a, b int) int { return a + b }\n"},
		{"repo-0", "src/placeholders.go", "go", strings.Join([]string{
			`password = "example-pass1"`,
			`db_pwd := "changeme123"`,
			`token = "dummyvalue99"`,
			`secret = "placeholder-secret"`,
			`api_key = "realkeyvalue1"`,
		}, "\n")},
		{"repo-1", "pkg/testdata/creds.go", "go", `password = "realpasswordvalue"`},
		{"repo-1", "pkg/config_test.go", "go", `token = "realtokenvalue1"`},
		{"repo-1", "deploy/fixtures/a.yaml", "yaml", `api_key: "yamlkeyvalue12"`},
		{"repo-1", "src/examples/demo.go", "go", `secret = "demosecretvalue"`},
		{"repo-1", "examples/root.go", "go", `secret = "rootexamplevalue"`},
		{"repo-1", "src/null_language.txt", "", `password = "nulllangpassword"`},
		{"repo-1", "src/env.local", "", "AWS_SECRET=abcdef123456\nclient_secret: 'zzzzzzzzzzzz'\n"},
		{"repo-2", "src/Zeta/Config.go", "go", `token = "zetatokenvalue"`},
		{"repo-2", "src/alpha/config.go", "go", `token = "alphatokenvalue"`},
		{"repo-2", "src/Ünï/é.go", "go", `password = "unicodepassword1"`},
		{"repo-2", "src/ünï/é.go", "go", `password = "lowerunicodepass1"`},
		{"repo-2", "SRC/upper.go", "go", `token = "uppercasepathtoken"`},
		{"repo-2", "src/a b/space name.go", "go", `secret = "spacedpathsecret1"`},
		{"repo-2", "src/very_long_line.go", "go", `token = "` + strings.Repeat("a", 5000) + `"`},
		{"repo-3", "infra/main.tf", "hcl", "provider \"aws\" {\n  access_key = \"AKIAZZZZZZZZZZZZZZZZ\"\n  secret_key = \"notmatchedbykind\"\n}\n"},
	}
}

var secretProofLineTemplates = []string{
	`aws_access_key_id = AKIA%016X`,
	`-----BEGIN OPENSSH PRIVATE KEY-----`,
	`slack_hook: xoxb-1234567890-abc%d`,
	`  api_key: "abcdef%dkeyvalue"`,
	`PASSWORD=Hunter%dxyz`,
	`client_secret = "s3cr3tvalue%d"`,
	`stripe = "sk_live_ABCDEFGH%d"`,
	`password = "example-pass%d"`,
	`token = "abc"`,
	`Authorization: Bearer abcdefghijk%d`,
	`db_pwd := "changeme%d"`,
	`key = "akiaabcdefgh%012d"`,
	"token=abcdefgh%d\r",
}

// secretProofBulkFiles builds deterministic filler so paging, offsets, and the
// 26-row default page have enough findings to cross page boundaries.
func secretProofBulkFiles(count int) []secretProofFile {
	dirs := []string{"src", "pkg/testdata", "src/examples", "internal", "cmd", "deploy/fixtures", "lib"}
	exts := []string{".go", "_test.go", ".py", ".yaml", ".env"}
	langs := []string{"go", "go", "python", "yaml", ""}
	files := make([]secretProofFile, 0, count)
	for i := 0; i < count; i++ {
		lines := make([]string, 0, 40)
		for j := 1; j <= 40; j++ {
			switch {
			case i%3 == 0 && (j == 5 || j == 27):
				lines = append(lines, fmt.Sprintf(secretProofLineTemplates[(i/3+j)%len(secretProofLineTemplates)], i*97+j))
			case j%17 == 0:
				lines = append(lines, fmt.Sprintf("// refresh the token before the secret expires %d", i))
			case j%23 == 0:
				lines = append(lines, fmt.Sprintf("func handlePassword(w http.ResponseWriter) { _ = w } // %d", j))
			default:
				lines = append(lines, fmt.Sprintf("x_%d := compute(%d)", j, i))
			}
		}
		files = append(files, secretProofFile{
			repo:     fmt.Sprintf("repo-%d", i%6),
			path:     fmt.Sprintf("%s/f_%04d%s", dirs[i%len(dirs)], i, exts[i%len(exts)]),
			language: langs[i%len(langs)],
			body:     strings.Join(lines, "\n"),
		})
	}
	return files
}

// secretProofRequests returns the 16 argument sets from the arbiter ruling plus
// the 8 unscoped limit-only sweep sets #7125 measured on ops-qa.
func secretProofRequests() []secretProofRequest {
	req := func(mut func(*codequery.HardcodedSecretInvestigationRequest)) codequery.HardcodedSecretInvestigationRequest {
		r := codequery.HardcodedSecretInvestigationRequest{Limit: 26}
		if mut != nil {
			mut(&r)
		}
		return r
	}
	sets := []secretProofRequest{
		{"default", req(nil)},
		{"limit 11", req(func(r *codequery.HardcodedSecretInvestigationRequest) { r.Limit = 11 })},
		{"limit 201", req(func(r *codequery.HardcodedSecretInvestigationRequest) { r.Limit = 201 })},
		{"offset 50", req(func(r *codequery.HardcodedSecretInvestigationRequest) { r.Offset = 50 })},
		{"offset 100 limit 201", req(func(r *codequery.HardcodedSecretInvestigationRequest) { r.Offset = 100; r.Limit = 201 })},
		{"offset past the end", req(func(r *codequery.HardcodedSecretInvestigationRequest) { r.Offset = 10000; r.Limit = 201 })},
		{"single kind", req(func(r *codequery.HardcodedSecretInvestigationRequest) { r.FindingKinds = []string{"aws_access_key"} })},
		{"two kinds", req(func(r *codequery.HardcodedSecretInvestigationRequest) {
			r.FindingKinds = []string{"api_token", "password_literal"}
		})},
		{"include suppressed", req(func(r *codequery.HardcodedSecretInvestigationRequest) { r.IncludeSuppressed = true })},
		{"repo filter", req(func(r *codequery.HardcodedSecretInvestigationRequest) { r.RepoID = "repo-1" })},
		{"repo filter with padding", req(func(r *codequery.HardcodedSecretInvestigationRequest) { r.RepoID = "  repo-2 " })},
		{"language filter", req(func(r *codequery.HardcodedSecretInvestigationRequest) { r.Language = "python" })},
		{"absent language", req(func(r *codequery.HardcodedSecretInvestigationRequest) { r.Language = "cobol" })},
		{"grant array", req(func(r *codequery.HardcodedSecretInvestigationRequest) {
			r.AllowedRepositoryIDs = []string{"repo-0", "repo-3"}
		})},
		{"repo wins over grant", req(func(r *codequery.HardcodedSecretInvestigationRequest) {
			r.RepoID = "repo-1"
			r.AllowedRepositoryIDs = []string{"repo-0"}
		})},
		{"repo kind include suppressed", req(func(r *codequery.HardcodedSecretInvestigationRequest) {
			r.RepoID = "repo-2"
			r.FindingKinds = []string{"secret_literal"}
			r.IncludeSuppressed = true
		})},
		{"limit 5000", req(func(r *codequery.HardcodedSecretInvestigationRequest) { r.Limit = 5000 })},
		{"limit 5000 include suppressed", req(func(r *codequery.HardcodedSecretInvestigationRequest) {
			r.Limit = 5000
			r.IncludeSuppressed = true
		})},
		{"rare kinds include suppressed", req(func(r *codequery.HardcodedSecretInvestigationRequest) {
			r.FindingKinds = []string{"slack_token", "private_key"}
			r.Limit = 5000
			r.IncludeSuppressed = true
		})},
		{"language and repo and kind", req(func(r *codequery.HardcodedSecretInvestigationRequest) {
			r.RepoID = "repo-0"
			r.Language = " go "
			r.FindingKinds = []string{"api_token"}
			r.Limit = 5000
		})},
	}
	for _, limit := range []int{10, 25, 50, 100, 200, 500, 1000, 2000} {
		limit := limit
		sets = append(sets, secretProofRequest{
			name: fmt.Sprintf("sweep limit %d", limit),
			req:  req(func(r *codequery.HardcodedSecretInvestigationRequest) { r.Limit = limit }),
		})
	}
	return sets
}
