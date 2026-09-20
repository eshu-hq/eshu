// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package submodule

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		body string
		want []Entry
	}{
		{
			name: "single submodule",
			body: "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n",
			want: []Entry{{Path: "lib/foo", URL: "https://github.com/example/libfoo.git"}},
		},
		{
			name: "multiple submodules",
			body: "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n" +
				"[submodule \"libbar\"]\n\tpath = lib/bar\n\turl = https://github.com/example/libbar.git\n",
			want: []Entry{
				{Path: "lib/foo", URL: "https://github.com/example/libfoo.git"},
				{Path: "lib/bar", URL: "https://github.com/example/libbar.git"},
			},
		},
		{
			name: "extra keys in a section are ignored",
			body: "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n\tbranch = main\n\tignore = dirty\n",
			want: []Entry{{Path: "lib/foo", URL: "https://github.com/example/libfoo.git"}},
		},
		{
			name: "section missing path yields no entry",
			body: "[submodule \"libfoo\"]\n\turl = https://github.com/example/libfoo.git\n",
			want: nil,
		},
		{
			name: "section missing url yields no entry",
			body: "[submodule \"libfoo\"]\n\tpath = lib/foo\n",
			want: nil,
		},
		{
			name: "one complete and one incomplete section",
			body: "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n" +
				"[submodule \"libbar\"]\n\tpath = lib/bar\n",
			want: []Entry{{Path: "lib/foo", URL: "https://github.com/example/libfoo.git"}},
		},
		{
			name: "blank lines and whole-line comments are skipped",
			body: "# top-level comment\n\n[submodule \"libfoo\"]\n\t; indented comment\n\tpath = lib/foo\n\n\turl = https://github.com/example/libfoo.git\n",
			want: []Entry{{Path: "lib/foo", URL: "https://github.com/example/libfoo.git"}},
		},
		{
			name: "relative submodule url is carried verbatim (unresolved)",
			body: "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = ../libfoo.git\n",
			want: []Entry{{Path: "lib/foo", URL: "../libfoo.git"}},
		},
		{
			name: "quoted values are unquoted",
			body: "[submodule \"lib foo\"]\n\tpath = \"lib/foo with space\"\n\turl = \"https://github.com/example/libfoo.git\"\n",
			want: []Entry{{Path: "lib/foo with space", URL: "https://github.com/example/libfoo.git"}},
		},
		{
			name: "key repeated in the same section last one wins",
			body: "[submodule \"libfoo\"]\n\tpath = lib/foo\n\tpath = lib/foo2\n\turl = https://github.com/example/libfoo.git\n",
			want: []Entry{{Path: "lib/foo2", URL: "https://github.com/example/libfoo.git"}},
		},
		{
			name: "key-value line before any section is ignored",
			body: "path = lib/orphan\nurl = https://github.com/example/orphan.git\n[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n",
			want: []Entry{{Path: "lib/foo", URL: "https://github.com/example/libfoo.git"}},
		},
		{
			name: "non-submodule section is skipped",
			body: "[core]\n\tpath = lib/orphan\n\turl = https://github.com/example/orphan.git\n[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n",
			want: []Entry{{Path: "lib/foo", URL: "https://github.com/example/libfoo.git"}},
		},
		{
			name: "bare submodule section with no subsection is skipped",
			body: "[submodule]\n\tpath = lib/orphan\n\turl = https://github.com/example/orphan.git\n[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n",
			want: []Entry{{Path: "lib/foo", URL: "https://github.com/example/libfoo.git"}},
		},
		{
			name: "crlf line endings are handled",
			body: "[submodule \"libfoo\"]\r\n\tpath = lib/foo\r\n\turl = https://github.com/example/libfoo.git\r\n",
			want: []Entry{{Path: "lib/foo", URL: "https://github.com/example/libfoo.git"}},
		},
		{
			name: "empty body yields no entries",
			body: "",
			want: nil,
		},
		{
			name: "only comments and blank lines yields no entries",
			body: "# just a comment\n\n   \n",
			want: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := Parse(testCase.body)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("Parse(%q) = %#v, want %#v", testCase.body, got, testCase.want)
			}
		})
	}
}
