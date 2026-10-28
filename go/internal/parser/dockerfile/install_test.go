// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import "testing"

func TestInstallEvidenceFromDockerfile(t *testing.T) {
	source := `ARG VERSION=1.2
FROM alpine:3.20
RUN apk add curl=8.0 && pecl install redis-${VERSION} \
    && docker-php-ext-install mysqli
ADD https://example.test/releases/app.tar.gz /tmp/app.tar.gz
`
	got := RuntimeMetadata(source)
	if len(got.Installs) != 4 {
		t.Fatalf("installs = %#v, want four declarations", got.Installs)
	}
	want := []Install{
		{Manager: "apk", Name: "curl", Version: "8.0", LineNumber: 3, Stage: "alpine"},
		{Manager: "pecl", Name: "redis", Version: "1.2", LineNumber: 3, Stage: "alpine"},
		{Manager: "docker-php-ext-install", Name: "mysqli", LineNumber: 3, Stage: "alpine"},
		{Manager: "url", Name: "example.test/releases/app.tar.gz", LineNumber: 5, Stage: "alpine"},
	}
	for i := range want {
		if got.Installs[i] != want[i] {
			t.Errorf("install[%d] = %#v, want %#v", i, got.Installs[i], want[i])
		}
	}
}

func TestInstallEvidenceFailsClosedOnCredentialURL(t *testing.T) {
	got := RuntimeMetadata("FROM alpine\nADD https://user:secret@example.test/app.tar.gz /tmp/\n")
	if len(got.Installs) != 0 || got.DroppedCredentials != 1 {
		t.Fatalf("got installs=%#v dropped=%d", got.Installs, got.DroppedCredentials)
	}
}

func TestInstallEvidenceSupportedCommands(t *testing.T) {
	tests := []struct{ source, manager, name, version string }{
		{"RUN apt-get install -y curl", "apt-get", "curl", ""},
		{"RUN apt install curl=8.1", "apt", "curl", "8.1"},
		{"RUN apk add libxml2=2.12", "apk", "libxml2", "2.12"},
		{"RUN yum install httpd", "yum", "httpd", ""},
		{"RUN dnf install httpd", "dnf", "httpd", ""},
		{"RUN npm install -g typescript@5.1", "npm", "typescript", "5.1"},
		{"RUN pip install my-package==2.0", "pip", "my-package", "2.0"},
		{"RUN pecl install redis-6.1", "pecl", "redis", "6.1"},
		{"RUN curl -o /tmp/tool 'https://example.test/tool?mirror=one'", "url", "example.test/tool", ""},
		{"RUN wget https://example.test/tool", "url", "example.test/tool", ""},
		{"RUN a2enmod php8.2", "a2enmod", "php8.2", ""},
	}
	for _, tc := range tests {
		t.Run(tc.source, func(t *testing.T) {
			got := RuntimeMetadata("FROM alpine\n" + tc.source + "\n")
			if len(got.Installs) != 1 {
				t.Fatalf("installs = %#v, unparsed = %#v", got.Installs, got.Unparsed)
			}
			row := got.Installs[0]
			if row.Manager != tc.manager || row.Name != tc.name || row.Version != tc.version {
				t.Fatalf("install = %#v, want %s %s %s", row, tc.manager, tc.name, tc.version)
			}
		})
	}
}

func TestInstallEvidenceHeredocAndUnparsed(t *testing.T) {
	got := RuntimeMetadata("FROM alpine\nRUN <<EOF\napk add curl\napt-get install libxml2=2.1\nEOF\nRUN apt-get install 'unterminated\n")
	if len(got.Installs) != 2 {
		t.Fatalf("installs = %#v", got.Installs)
	}
	if len(got.Unparsed) != 1 || got.Unparsed[0].Reason != "invalid_shell_words" {
		t.Fatalf("unparsed = %#v", got.Unparsed)
	}
}

func TestInstallEvidenceDoesNotGuessUnsafeInput(t *testing.T) {
	got := RuntimeMetadata("FROM alpine\nRUN pip install my-package && curl https://example.test/?token=secret\nRUN apt-get install $MISSING\n")
	if len(got.Installs) != 1 || got.Installs[0].Name != "my-package" {
		t.Fatalf("installs = %#v", got.Installs)
	}
	if got.DroppedCredentials != 1 {
		t.Fatalf("dropped = %d", got.DroppedCredentials)
	}
	if len(got.Unparsed) != 1 || got.Unparsed[0].Reason != "unresolved_variable" {
		t.Fatalf("unparsed = %#v", got.Unparsed)
	}
}
