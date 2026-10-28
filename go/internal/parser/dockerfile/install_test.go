// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestInstallEvidenceFromDockerfile(t *testing.T) {
	source := `ARG VERSION=1.2
FROM alpine:3.20
ARG VERSION
RUN apk add curl=8.0 && pecl install redis-${VERSION} \
    && docker-php-ext-install mysqli
ADD https://example.test/releases/app.tar.gz /tmp/app.tar.gz
`
	got := RuntimeMetadata(source)
	if len(got.Installs) != 4 {
		t.Fatalf("installs = %#v, want four declarations", got.Installs)
	}
	want := []Install{
		{Manager: "apk", Name: "curl", Version: "8.0", LineNumber: 4, Stage: "alpine"},
		{Manager: "pecl", Name: "redis", Version: "1.2", LineNumber: 4, Stage: "alpine"},
		{Manager: "docker-php-ext-install", Name: "mysqli", LineNumber: 4, Stage: "alpine"},
		{Manager: "url", Name: "example.test/releases/app.tar.gz", LineNumber: 6, Stage: "alpine"},
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

func TestInstallEvidenceRespectsARGAndENVStageScope(t *testing.T) {
	source := `ARG GLOBAL=curl
FROM alpine AS base
RUN apk add $GLOBAL
ARG GLOBAL
ENV TOOL=git
RUN apk add $GLOBAL $TOOL
FROM alpine AS unrelated
RUN apk add $GLOBAL $TOOL
FROM base AS child
RUN apk add $GLOBAL $TOOL
`
	got := RuntimeMetadata(source)
	want := []Install{
		{Manager: "apk", Name: "curl", LineNumber: 6, Stage: "base"},
		{Manager: "apk", Name: "git", LineNumber: 6, Stage: "base"},
		{Manager: "apk", Name: "curl", LineNumber: 10, Stage: "child"},
		{Manager: "apk", Name: "git", LineNumber: 10, Stage: "child"},
	}
	if len(got.Installs) != len(want) {
		t.Fatalf("installs = %#v, want %#v", got.Installs, want)
	}
	for i := range want {
		if got.Installs[i] != want[i] {
			t.Errorf("install[%d] = %#v, want %#v", i, got.Installs[i], want[i])
		}
	}
	if len(got.Unparsed) < 2 {
		t.Fatalf("unparsed = %#v, want unresolved stages recorded", got.Unparsed)
	}
}

func TestInstallEvidenceENVOverridesStageARG(t *testing.T) {
	got := RuntimeMetadata("FROM alpine\nARG PACKAGE=curl\nENV PACKAGE=git\nRUN apk add $PACKAGE\n")
	if len(got.Installs) != 1 || got.Installs[0].Name != "git" {
		t.Fatalf("installs = %#v, want ENV value git", got.Installs)
	}
}

func TestInstallEvidenceHeredocExecutableAndDataBoundaries(t *testing.T) {
	cases := []struct {
		name, source string
		want         []string
	}{
		{"simple executable", "FROM alpine\nRUN <<EOF\napk add curl\napt-get install git\nEOF\n", []string{"curl", "git"}},
		{"data redirect", "FROM alpine\nRUN <<EOF cat > /tmp/script\napt-get install fake\nEOF\n", nil},
		{"copy data", "FROM alpine\nCOPY <<EOF /tmp/script\nRUN apt-get install fake\nEOF\n", nil},
		{"shell data redirect", "FROM alpine\nRUN cat <<'EOF'\nRUN apt-get install fake\nEOF\n", nil},
		{"control flow", "FROM alpine\nRUN <<EOF\nif false; then\napt-get install fake\nfi\nEOF\n", nil},
		{"nested data", "FROM alpine\nRUN <<EOF\ncat > /tmp/script <<'DATA'\napt-get install fake\nDATA\nEOF\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RuntimeMetadata(tc.source)
			if len(got.Installs) != len(tc.want) {
				t.Fatalf("installs = %#v, want names %#v", got.Installs, tc.want)
			}
			for i, name := range tc.want {
				if got.Installs[i].Name != name {
					t.Errorf("install[%d] = %#v, want %s", i, got.Installs[i], name)
				}
			}
			if tc.want == nil && tc.name != "copy data" && len(got.Unparsed) == 0 {
				t.Fatal("unsupported heredoc must report bounded unparsed evidence")
			}
		})
	}
}

func TestInstallEvidenceShellCommentBoundary(t *testing.T) {
	got := RuntimeMetadata("FROM alpine\nRUN apt-get install curl # explanatory note\nRUN apt-get install 'curl#debug'\n")
	if len(got.Installs) != 2 || got.Installs[0].Name != "curl" || got.Installs[1].Name != "curl#debug" {
		t.Fatalf("installs = %#v, want exact quoted and unquoted comment handling", got.Installs)
	}
}

func TestInstallEvidenceDropsCredentialedOptionsAndURLPaths(t *testing.T) {
	const marker = "exampleSecretToken123"
	cases := []struct {
		name, instruction string
		wantName          string
	}{
		{"pip index option", "RUN pip install --index-url https://user:" + marker + "@private.example/simple requests", "requests"},
		{"pip attached index option", "RUN pip install --index-url=https://user:" + marker + "@private.example/simple requests", "requests"},
		{"pip direct URL", "RUN pip install https://user:" + marker + "@private.example/simple", ""},
		{"unknown option value", "RUN pip install --mystery https://user:" + marker + "@private.example/simple requests", ""},
		{"unknown option after credential", "RUN pip install https://user:" + marker + "@private.example/simple --mystery requests", ""},
		{"ADD token path", "ADD https://example.test/token=" + marker + "/file.tar.gz /tmp/", ""},
		{"ADD encoded token path", "ADD https://example.test/%74oken%3D" + marker + "/file.tar.gz /tmp/", ""},
		{"ADD double encoded token path", "ADD https://example.test/%2574oken%253D" + marker + "/file.tar.gz /tmp/", ""},
		{"ADD unknown option before credential URL", "ADD --mystery=one https://user:" + marker + "@private.example/file.tar.gz /tmp/", ""},
		{"curl encoded token path", "RUN curl https://example.test/token%3D" + marker + "/file.tar.gz", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RuntimeMetadata("FROM alpine\n" + tc.instruction + "\n")
			if got.DroppedCredentials != 1 {
				t.Fatalf("dropped credentials = %d, want 1", got.DroppedCredentials)
			}
			if tc.wantName == "" && len(got.Installs) != 0 || tc.wantName != "" && (len(got.Installs) != 1 || got.Installs[0].Name != tc.wantName) {
				t.Fatalf("installs = %#v, want name %q", got.Installs, tc.wantName)
			}
			encoded, err := json.Marshal(got.Map())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), marker) {
				t.Fatal("secret marker reached normalized Dockerfile evidence")
			}
		})
	}
}

func TestInstallEvidenceDropsSensitiveVariableExpansionAcrossARGAndENV(t *testing.T) {
	const marker = "exampleSecretToken123"
	source := "FROM alpine\n" +
		"ARG API_TOKEN=" + marker + "\n" +
		"RUN pip install $API_TOKEN\n" +
		"ARG PACKAGE_ALIAS=$API_TOKEN\n" +
		"RUN apk add $PACKAGE_ALIAS\n" +
		"ENV MIRROR=$PACKAGE_ALIAS\n" +
		"ADD https://example.test/$MIRROR/file.tar.gz /tmp/\n" +
		"ENV access_key=" + marker + "\n" +
		"RUN apk add $access_key\n"
	got := RuntimeMetadata(source)
	if len(got.Installs) != 0 || got.DroppedCredentials != 4 {
		t.Fatalf("installs = %#v, dropped = %d, want no installs and four credential drops", got.Installs, got.DroppedCredentials)
	}
	rows, _ := json.Marshal(got.Map()["dockerfile_installs"])
	if strings.Contains(string(rows), marker) {
		t.Fatal("sensitive variable value reached install rows")
	}
}

func TestInstallEvidenceCountsSensitiveVariableInOptionValues(t *testing.T) {
	got := RuntimeMetadata("FROM alpine\nARG API_TOKEN=exampleSecretToken123\n" +
		"RUN pip install --index-url $API_TOKEN requests\n" +
		"ADD --checksum=$API_TOKEN https://example.test/tool.tar.gz /tmp/\n")
	if got.DroppedCredentials != 2 || len(got.Installs) != 2 ||
		got.Installs[0].Name != "requests" || got.Installs[1].Name != "example.test/tool.tar.gz" {
		t.Fatalf("installs = %#v, dropped = %d, want two safe rows and two credential drops", got.Installs, got.DroppedCredentials)
	}
}

func TestInstallEvidenceADDMultipleFlags(t *testing.T) {
	got := RuntimeMetadata("FROM alpine\nADD --checksum=sha256:abc --chmod=0644 https://example.test/pkg.tar.gz /tmp/\n")
	if len(got.Installs) != 1 || got.Installs[0].Name != "example.test/pkg.tar.gz" {
		t.Fatalf("installs = %#v, want flagged URL source", got.Installs)
	}
	unknown := RuntimeMetadata("FROM alpine\nADD --unknown=value https://example.test/pkg.tar.gz /tmp/\n")
	if len(unknown.Installs) != 0 || len(unknown.Unparsed) != 1 {
		t.Fatalf("unknown ADD flag evidence = installs %#v unparsed %#v", unknown.Installs, unknown.Unparsed)
	}
}

func BenchmarkInstallEvidenceStageScope(b *testing.B) {
	ordinary := "ARG TOOL=curl\nFROM alpine AS base\nARG TOOL\nRUN apk add $TOOL\nFROM base AS final\nRUN apk add $TOOL\n"
	var chained strings.Builder
	chained.WriteString("FROM alpine AS s0\nARG PKG0=curl\n")
	for i := 1; i <= 1000; i++ {
		chained.WriteString("FROM s")
		chained.WriteString(strconv.Itoa(i - 1))
		chained.WriteString(" AS s")
		chained.WriteString(strconv.Itoa(i))
		chained.WriteString("\nARG PKG")
		chained.WriteString(strconv.Itoa(i))
		chained.WriteString("=curl\n")
	}
	chained.WriteString("RUN apk add $PKG0\n")
	repeated := chained.String() + strings.Repeat("RUN apk add $PKG0\n", 99)
	for _, tc := range []struct{ name, source string }{
		{"ordinary", ordinary},
		{"inherited_1000", chained.String()},
		{"inherited_1000_repeated_100", repeated},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = RuntimeMetadata(tc.source)
			}
		})
	}
}

func BenchmarkInstallEvidenceEcosystemsDockerfiles(b *testing.B) {
	fixtures := []struct {
		path   string
		stages int
	}{
		{"api-svc/Dockerfile", 1},
		{"container-base-lineage/Dockerfile", 2},
		{"container-ci-lineage/Dockerfile", 2},
		{"deployable-source/Dockerfile", 1},
		{"jenkins-ci-pipelines/deployable-config/Dockerfile", 1},
	}
	bodies := make([]string, 0, len(fixtures))
	for _, fixture := range fixtures {
		path := filepath.Join("..", "..", "..", "..", "tests", "fixtures", "ecosystems", fixture.path)
		body, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		if got := len(RuntimeMetadata(string(body)).Stages); got != fixture.stages {
			b.Fatalf("%s has %d stages, want %d", fixture.path, got, fixture.stages)
		}
		bodies = append(bodies, string(body))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, body := range bodies {
			_ = RuntimeMetadata(body)
		}
	}
}

func TestInstallEvidenceConsumesKnownOptionValues(t *testing.T) {
	got := RuntimeMetadata(`FROM alpine
RUN pip install --index-url https://mirror.example/simple --extra-index-url https://backup.example/simple requests
RUN apt-get install -o Debug::NoLocking=true -y curl
RUN apk add --virtual builddeps gcc
RUN npm install -g --registry https://registry.example typescript@5.1
RUN docker-php-ext-install -j4 pdo_mysql
RUN docker-php-ext-configure gd --with-freetype --with-jpeg=/usr/local
RUN docker-php-ext-install --ini-name custom.ini mysqli
RUN docker-php-ext-enable --ini-name 0-opcache.ini opcache
`)
	want := []Install{
		{Manager: "pip", Name: "requests", LineNumber: 2, Stage: "alpine"},
		{Manager: "apt-get", Name: "curl", LineNumber: 3, Stage: "alpine"},
		{Manager: "apk", Name: "gcc", LineNumber: 4, Stage: "alpine"},
		{Manager: "npm", Name: "typescript", Version: "5.1", LineNumber: 5, Stage: "alpine"},
		{Manager: "docker-php-ext-install", Name: "pdo_mysql", LineNumber: 6, Stage: "alpine"},
		{Manager: "docker-php-ext-configure", Name: "gd", LineNumber: 7, Stage: "alpine"},
		{Manager: "docker-php-ext-install", Name: "mysqli", LineNumber: 8, Stage: "alpine"},
		{Manager: "docker-php-ext-enable", Name: "opcache", LineNumber: 9, Stage: "alpine"},
	}
	if len(got.Installs) != len(want) || len(got.Unparsed) != 0 {
		t.Fatalf("installs = %#v, unparsed = %#v, want %#v", got.Installs, got.Unparsed, want)
	}
	for i := range want {
		if got.Installs[i] != want[i] {
			t.Errorf("install[%d] = %#v, want %#v", i, got.Installs[i], want[i])
		}
	}
}

func TestInstallEvidenceHeredocBoundaryReturnsToDockerfileInstructions(t *testing.T) {
	got := RuntimeMetadata("FROM alpine\nRUN <<EOF\napk add curl\nEOF\nRUN apk add git\n")
	if len(got.Installs) != 2 || got.Installs[0].Name != "curl" || got.Installs[1].Name != "git" {
		t.Fatalf("installs = %#v, want heredoc curl then ordinary RUN git", got.Installs)
	}
}
