// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"bytes"
	"fmt"
)

// buildDirectoryPackagesProps returns a valid Directory.Packages.props of
// >= minLOC lines: a literal PropertyGroup, then one ItemGroup of PackageVersion
// entries half of which resolve through $(Property) references. A file this
// long also passes the scanner's row cap, so the case measures the capped path
// a very large central file takes.
func buildDirectoryPackagesProps(minLOC int) []byte {
	var b bytes.Buffer
	b.WriteString("<Project>\n  <PropertyGroup>\n")
	b.WriteString("    <ManagePackageVersionsCentrally>true</ManagePackageVersionsCentrally>\n")
	b.WriteString("    <SharedVersion>1.2.3</SharedVersion>\n  </PropertyGroup>\n  <ItemGroup>\n")
	for i := 0; bytes.Count(b.Bytes(), []byte("\n")) < minLOC; i++ {
		if i%2 == 0 {
			fmt.Fprintf(&b, "    <PackageVersion Include=\"Example.Package%04d\" Version=\"1.%d.0\" />\n", i, i%10)
		} else {
			fmt.Fprintf(&b, "    <PackageVersion Include=\"Example.Package%04d\" Version=\"$(SharedVersion)\" />\n", i)
		}
	}
	b.WriteString("  </ItemGroup>\n</Project>\n")
	return b.Bytes()
}

// buildDirectoryBuildProps returns a valid Directory.Build.props of >= minLOC
// lines: shared properties followed by PackageReference entries.
func buildDirectoryBuildProps(minLOC int) []byte {
	var b bytes.Buffer
	b.WriteString("<Project>\n  <PropertyGroup>\n")
	b.WriteString("    <TargetFramework>net8.0</TargetFramework>\n    <Version>1.4.0</Version>\n  </PropertyGroup>\n  <ItemGroup>\n")
	for i := 0; bytes.Count(b.Bytes(), []byte("\n")) < minLOC; i++ {
		fmt.Fprintf(&b, "    <PackageReference Include=\"Example.Analyzer%04d\" Version=\"1.%d.0\" PrivateAssets=\"all\" />\n", i, i%10)
	}
	b.WriteString("  </ItemGroup>\n</Project>\n")
	return b.Bytes()
}
