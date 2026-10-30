// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// updateASCIIGolden rewrites testdata/ascii_keys_golden.json from the current
// code. The committed file was generated on the code before #9584, so a run
// without the flag is a before/after differential for ASCII names.
var updateASCIIGolden = flag.Bool("update-ascii-golden", false, "rewrite testdata/ascii_keys_golden.json")

// asciiNameCorpus is a set of real-looking ASCII manifest and advisory names
// for every ecosystem the key functions accept, including alias spellings,
// PyPI separator variants, Maven group:artifact and group/artifact forms, npm
// scopes, Go module paths with uppercase, Composer vendor/name, and Hex
// organization/name.
var asciiNameCorpus = []struct {
	Ecosystem string
	Name      string
}{
	{"npm", "react"},
	{"npm", "React-DOM"},
	{"npm", "@Babel/Core"},
	{"npm", "@types/node"},
	{"node", "lodash.merge"},
	{"typescript", "@angular/CLI"},
	{"npm", " left-pad "},
	{"pypi", "Django"},
	{"pypi", "Friendly_Bard...Plugin"},
	{"python", "zope.interface"},
	{"pip", "Flask-SQLAlchemy"},
	{"pypi", "ruamel.yaml.clib"},
	{"pypi", "PyYAML"},
	{"pypi", "typing_extensions"},
	{"gomod", "github.com/Azure/azure-sdk-for-go"},
	{"go", "github.com/BurntSushi/toml"},
	{"golang", "golang.org/x/mod"},
	{"gomod", "github.com/aws/aws-sdk-go-v2/service/s3"},
	{"gomod", "gopkg.in/yaml.v3"},
	{"gomod", "github.com/Masterminds/semver/v3"},
	{"maven", "org.apache.maven:maven-core"},
	{"maven", "org.apache.maven/maven-core"},
	{"gradle", "com.Google.Guava:Guava"},
	{"java", "org.springframework.boot:spring-boot-starter-web"},
	{"maven", "junit"},
	{"maven", "io.netty/netty-all"},
	{"nuget", "Serilog"},
	{"nuget", "Newtonsoft.Json"},
	{"dotnet", "Microsoft.Extensions.Logging"},
	{".net", "AutoMapper.Extensions.Microsoft.DependencyInjection"},
	{"composer", "Symfony/Console"},
	{"packagist", "monolog/monolog"},
	{"php", "laravel/framework"},
	{"composer", "single-segment"},
	{"rubygems", "Rails"},
	{"gem", "activesupport"},
	{"ruby", "Nokogiri"},
	{"cargo", "Serde_JSON"},
	{"crates.io", "tokio"},
	{"rust", "regex-syntax"},
	{"swift", "github.com/Apple/swift-nio"},
	{"spm", "Alamofire"},
	{"hex", "Phoenix"},
	{"hexpm", "ecto_sql"},
	{"hex", "Acme:Plug"},
	{"hex", "acme/Cowboy"},
	{"pub", "Flutter_Bloc"},
	{"dart", "http"},
	{"pub.dev", "provider"},
	{"os", "OpenSSL"},
	{"debian", "libc6"},
	{"apk", "musl"},
	{"generic", "Some/Thing"},
	{"made-up", "Package"},
	{"npm", ""},
	{"npm", "   "},
}

// asciiRegistryCorpus covers ConsumptionKeysForRegistryIdentity inputs.
var asciiRegistryCorpus = []struct {
	Ecosystem, RawName, NormalizedName, Namespace string
}{
	{"npm", "@Babel/Core", "@babel/core", "babel"},
	{"pypi", "Friendly_Bard", "friendly-bard", ""},
	{"maven", "Guava", "guava", "com.Google.Guava"},
	{"maven", "maven-core", "maven-core", "org.apache.maven"},
	{"nuget", "Serilog", "serilog", ""},
	{"composer", "Console", "symfony/console", "symfony"},
	{"hex", "Plug", "plug", "Acme"},
	{"swift", "swift-nio", "swift-nio", "github.com/apple"},
	{"gomod", "github.com/Azure/go-autorest", "github.com/Azure/go-autorest", "github.com/Azure"},
}

var asciiPURLCorpus = []string{
	"pkg:npm/%40babel/core@7.0.0", "pkg:npm/React@18", "pkg:pypi/Django@4.2",
	"pkg:maven/org.apache.maven/maven-core@3.9.0", "pkg:nuget/Serilog@3.0.0",
	"pkg:golang/github.com/BurntSushi/toml@v1.3.0", "pkg:gem/Rails@7.1", "pkg:cargo/Serde@1",
	"pkg:composer/Symfony/Console@6", "pkg:hex/acme/Plug@1", "pkg:pub/Flutter_Bloc@8",
	"pkg:swift/github.com/Apple/swift-nio@2", "not-a-purl",
}

var asciiPackageIDCorpus = []string{
	"npm://registry.npmjs.org/@babel/core", "pypi://pypi.org/simple/django",
	"nuget://api.nuget.org/v3/index.json/serilog", "gomod://proxy.golang.org/github.com/BurntSushi/toml",
	"maven://repo.maven.apache.org/maven2/org.apache.maven:maven-core", "cargo://crates.io/serde",
	"npm://custom.example/foo",
}

// asciiGolden is every key and name set the corpora produce.
type asciiGolden struct {
	ConsumptionKeys         map[string][]ConsumptionKey `json:"consumption_keys"`
	ManifestEntityNames     map[string][]string         `json:"manifest_entity_names"`
	RegistryIdentityKeys    map[string][]ConsumptionKey `json:"registry_identity_keys"`
	PURLKeys                map[string][]ConsumptionKey `json:"purl_keys"`
	PackageIDKeys           map[string][]ConsumptionKey `json:"package_id_keys"`
	NormalizedPackageIDs    map[string]string           `json:"normalized_package_ids"`
	NormalizedEcosystemName map[string]Ecosystem        `json:"normalized_ecosystems"`
}

func buildASCIIGolden() asciiGolden {
	golden := asciiGolden{
		ConsumptionKeys:         map[string][]ConsumptionKey{},
		ManifestEntityNames:     map[string][]string{},
		RegistryIdentityKeys:    map[string][]ConsumptionKey{},
		PURLKeys:                map[string][]ConsumptionKey{},
		PackageIDKeys:           map[string][]ConsumptionKey{},
		NormalizedPackageIDs:    map[string]string{},
		NormalizedEcosystemName: map[string]Ecosystem{},
	}
	for _, entry := range asciiNameCorpus {
		label := entry.Ecosystem + "|" + entry.Name
		golden.ConsumptionKeys[label] = ConsumptionKeys(entry.Ecosystem, entry.Name)
		ecosystem := NormalizeEcosystem(Ecosystem(entry.Ecosystem))
		golden.NormalizedEcosystemName[entry.Ecosystem] = ecosystem
		golden.ManifestEntityNames[label] = ManifestEntityNameCandidates(ecosystem, entry.Name)
		raw, namespace := consumptionRawNameAndNamespace(ecosystem, entry.Name)
		identity, err := Normalize(RawIdentity{Ecosystem: ecosystem, Registry: DefaultRegistry(ecosystem), RawName: raw, Namespace: namespace})
		if err != nil {
			golden.NormalizedPackageIDs[label] = "error"
			continue
		}
		golden.NormalizedPackageIDs[label] = identity.PackageID
	}
	for _, entry := range asciiRegistryCorpus {
		label := fmt.Sprintf("%s|%s|%s|%s", entry.Ecosystem, entry.RawName, entry.NormalizedName, entry.Namespace)
		golden.RegistryIdentityKeys[label] = ConsumptionKeysForRegistryIdentity(entry.Ecosystem, entry.RawName, entry.NormalizedName, entry.Namespace)
	}
	for _, purl := range asciiPURLCorpus {
		golden.PURLKeys[purl] = ConsumptionKeysFromPURL(purl)
	}
	for _, packageID := range asciiPackageIDCorpus {
		golden.PackageIDKeys[packageID] = ConsumptionKeysFromPackageID(packageID)
	}
	return golden
}

// TestASCIIKeysMatchPre9584Golden proves the #9584 fold change leaves every
// ASCII key byte-identical: the golden file was written by the code before
// the change.
func TestASCIIKeysMatchPre9584Golden(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "ascii_keys_golden.json")
	got, err := json.MarshalIndent(buildASCIIGolden(), "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	got = append(got, '\n')
	if *updateASCIIGolden {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ASCII keys drifted from the pre-#9584 golden %s; rerun with -update-ascii-golden only on purpose\n got: %s", path, got)
	}
}
