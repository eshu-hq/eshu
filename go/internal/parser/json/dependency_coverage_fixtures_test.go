// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

func packageJSONCoveredFixture() coveredFixture {
	return coveredFixture{
		parser: parseJSONFixture,
		body: `{
  "name": "demo",
  "dependencies": {"lodash": "^4.17.21"},
  "devDependencies": {"vitest": "^2.0.0"}
}`,
		expectedDependencies:  map[string]string{"lodash": "^4.17.21", "vitest": "^2.0.0"},
		expectedPackageMgr:    "npm",
		expectedSection:       "dependencies",
		expectScopeSplit:      true,
		expectedDevDependency: "vitest",
	}
}

func packageLockCoveredFixture() coveredFixture {
	return coveredFixture{
		parser: parseJSONFixture,
		body: `{
  "name": "demo",
  "lockfileVersion": 3,
  "packages": {
    "": {"dependencies": {"lodash": "^4.17.21"}},
    "node_modules/lodash": {"version": "4.17.21"}
  }
}`,
		expectedDependencies: map[string]string{"lodash": "4.17.21"},
		expectedPackageMgr:   "npm",
		expectedSection:      "package-lock",
		transitiveBody: `{
  "lockfileVersion": 3,
  "packages": {
    "": {"dependencies": {"vite": "^5.0.0"}},
    "node_modules/vite": {"version": "5.0.0", "dependencies": {"rollup": "^4.0.0"}},
    "node_modules/rollup": {"version": "4.0.0"}
  }
}`,
	}
}

func composerManifestCoveredFixture() coveredFixture {
	return coveredFixture{
		parser: parseJSONFixture,
		body: `{
  "name": "demo/app",
  "require": {"monolog/monolog": "^2.0"},
  "require-dev": {"phpunit/phpunit": "^9.0"}
}`,
		expectedDependencies:  map[string]string{"monolog/monolog": "^2.0", "phpunit/phpunit": "^9.0"},
		expectedPackageMgr:    "composer",
		expectedSection:       "require",
		expectScopeSplit:      true,
		expectedDevDependency: "phpunit/phpunit",
	}
}

func composerLockCoveredFixture() coveredFixture {
	return coveredFixture{
		parser: parseJSONFixture,
		body: `{
  "packages": [
    {"name": "monolog/monolog", "version": "2.9.1"}
  ],
  "packages-dev": [
    {"name": "phpunit/phpunit", "version": "9.6.13"}
  ]
}`,
		expectedDependencies:  map[string]string{"monolog/monolog": "2.9.1", "phpunit/phpunit": "9.6.13"},
		expectedPackageMgr:    "composer",
		expectedSection:       "packages",
		expectScopeSplit:      true,
		expectedDevDependency: "phpunit/phpunit",
		transitiveBody: `{
  "packages": [
    {"name": "symfony/http-kernel", "version": "6.4.1", "require": {"symfony/event-dispatcher": "^6.4"}},
    {"name": "symfony/event-dispatcher", "version": "6.4.0"}
  ]
}`,
	}
}

func gemfileCoveredFixture() coveredFixture {
	return coveredFixture{
		parser: parseRubyFixture,
		body: `source "https://rubygems.org"
gem "rails", "~> 7.1"
group :development, :test do
  gem "rspec-rails", "~> 6.1"
end
`,
		expectedDependencies:  map[string]string{"rails": "~> 7.1", "rspec-rails": "~> 6.1"},
		expectedPackageMgr:    "rubygems",
		expectedSection:       "default",
		expectScopeSplit:      true,
		expectedDevDependency: "rspec-rails",
	}
}

func gemfileLockCoveredFixture() coveredFixture {
	return coveredFixture{
		parser: parseRubyFixture,
		body: `GEM
  remote: https://rubygems.org/
  specs:
    rails (7.1.3)
      rack (>= 2.2.4)
    rack (2.2.8)

DEPENDENCIES
  rails (~> 7.1)
`,
		expectedDependencies: map[string]string{"rails": "7.1.3", "rack": "2.2.8"},
		expectedPackageMgr:   "rubygems",
		expectedSection:      "gemfile.lock",
		transitiveBody: `GEM
  remote: https://rubygems.org/
  specs:
    rails (7.1.3)
      rack (>= 2.2.4)
    rack (2.2.8)

DEPENDENCIES
  rails (~> 7.1)
`,
	}
}

func nugetLockCoveredFixture() coveredFixture {
	return coveredFixture{
		parser: parseJSONFixture,
		body: `{
  "version": 1,
  "dependencies": {
    "net8.0": {
      "Newtonsoft.Json": {
        "type": "Direct",
        "requested": "[13.0.3, )",
        "resolved": "13.0.3"
      }
    }
  }
}`,
		expectedDependencies: map[string]string{"Newtonsoft.Json": "13.0.3"},
		expectedPackageMgr:   "nuget",
		expectedSection:      "packages.lock.json:net8.0",
		transitiveBody: `{
  "version": 1,
  "dependencies": {
    "net8.0": {
      "Newtonsoft.Json": {
        "type": "Direct",
        "requested": "[13.0.3, )",
        "resolved": "13.0.3",
        "dependencies": {"System.Text.Encodings.Web": "[8.0.0, )"}
      },
      "System.Text.Encodings.Web": {
        "type": "Transitive",
        "resolved": "8.0.0"
      }
    }
  }
}`,
	}
}

func swiftPackageResolvedCoveredFixture() coveredFixture {
	return coveredFixture{
		parser: parseJSONFixture,
		body: `{
  "originHash": "example",
  "pins": [
    {
      "identity": "swift-argument-parser",
      "kind": "remoteSourceControl",
      "location": "https://github.com/apple/swift-argument-parser.git",
      "state": {
        "revision": "0123456789abcdef0123456789abcdef01234567",
        "version": "1.2.3"
      }
    }
  ],
  "version": 2
}`,
		expectedDependencies: map[string]string{"github.com/apple/swift-argument-parser": "1.2.3"},
		expectedPackageMgr:   "swift",
		expectedSection:      "Package.resolved",
	}
}

func mavenCoveredFixture() coveredFixture {
	return coveredFixture{
		parser: parseMavenFixture,
		body: `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
  <properties><spring.version>5.3.20</spring.version></properties>
  <dependencies>
    <dependency><groupId>org.springframework</groupId><artifactId>spring-core</artifactId><version>${spring.version}</version></dependency>
    <dependency><groupId>junit</groupId><artifactId>junit</artifactId><version>4.13.2</version><scope>test</scope></dependency>
  </dependencies>
</project>`,
		expectedDependencies: map[string]string{
			"org.springframework:spring-core": "5.3.20",
			"junit:junit":                     "4.13.2",
		},
		expectedPackageMgr:    "maven",
		expectedSection:       "dependencies",
		expectScopeSplit:      true,
		expectedDevDependency: "junit:junit",
	}
}

func gradleCoveredFixture() coveredFixture {
	return coveredFixture{
		parser: parseGradleFixture,
		body: `plugins { id 'java' }
dependencies {
    implementation 'org.springframework:spring-core:5.3.20'
    testImplementation 'junit:junit:4.13.2'
}`,
		expectedDependencies: map[string]string{
			"org.springframework:spring-core": "5.3.20",
			"junit:junit":                     "4.13.2",
		},
		expectedPackageMgr:    "gradle",
		expectedSection:       "implementation",
		expectScopeSplit:      true,
		expectedDevDependency: "junit:junit",
	}
}

func gradleKTSCoveredFixture() coveredFixture {
	fixture := gradleCoveredFixture()
	fixture.body = `plugins { java }
dependencies {
    implementation("org.springframework:spring-core:5.3.20")
    testImplementation("junit:junit:4.13.2")
}`
	return fixture
}

func pipfileLockCoveredFixture() coveredFixture {
	return coveredFixture{
		parser: parseJSONFixture,
		body: `{
  "_meta": {"sources": [{"name": "pypi"}]},
  "default": {"requests": {"version": "==2.31.0"}},
  "develop": {"pytest": {"version": "==7.4.4"}}
}`,
		expectedDependencies:  map[string]string{"requests": "2.31.0", "pytest": "7.4.4"},
		expectedPackageMgr:    "pypi",
		expectedSection:       "default",
		expectScopeSplit:      true,
		expectedDevDependency: "pytest",
	}
}

func yarnCoveredFixture() coveredFixture {
	return coveredFixture{
		parser: parseNodeLockfileFixture,
		body: `# yarn lockfile v1

lodash@^4.17.21:
  version "4.17.21"

vite@^5.0.0:
  version "5.0.0"
  dependencies:
    rollup "^4.0.0"

rollup@^4.0.0:
  version "4.0.0"
`,
		expectedDependencies: map[string]string{"lodash": "4.17.21", "vite": "5.0.0", "rollup": "4.0.0"},
		expectedPackageMgr:   "npm",
		expectedSection:      "yarn.lock",
	}
}

func pnpmCoveredFixture() coveredFixture {
	return coveredFixture{
		parser: parseNodeLockfileFixture,
		body: `lockfileVersion: '6.0'
importers:
  .:
    dependencies:
      lodash:
        specifier: ^4.17.21
        version: 4.17.21
    devDependencies:
      vitest:
        specifier: ^2.0.0
        version: 2.0.0
packages:
  /lodash@4.17.21:
    resolution: {integrity: sha512-AbCdEf==}
  /vitest@2.0.0:
    resolution: {integrity: sha512-Vit==}
    dependencies:
      vite: 5.0.0
  /vite@5.0.0:
    resolution: {integrity: sha512-V==}
`,
		expectedDependencies:  map[string]string{"lodash": "4.17.21", "vitest": "2.0.0", "vite": "5.0.0"},
		expectedPackageMgr:    "npm",
		expectedSection:       "runtime",
		expectScopeSplit:      true,
		expectedDevDependency: "vitest",
	}
}
