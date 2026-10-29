// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import "strings"

// installActionIndex skips recognized manager options before the action word.
// The returned index is relative to the full shell word slice.
func installActionIndex(manager string, args []string, scope *installScope) (int, string, int) {
	for index := 0; index < len(args); index++ {
		word := args[index]
		if !strings.HasPrefix(word, "-") {
			return index + 1, "", 0
		}
		name, _, attached := strings.Cut(word, "=")
		arity := installOptionArity(manager, name)
		if arity == unknownInstallOption || arity == flagInstallOption && attached {
			return 0, "unsupported_option", countCredentialValues(args, scope)
		}
		if arity == valueInstallOption && !attached {
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return 0, "missing_option_value", countCredentialValues(args, scope)
			}
		}
	}
	return len(args) + 1, "", 0
}

type installOptions struct {
	packages []string
	global   bool
	dropped  int
	reason   string
}

const (
	unknownInstallOption = iota
	flagInstallOption
	valueInstallOption
)

// parseInstallOptions consumes recognized option values before package names
// are considered. Unsupported options reject the entire command, preventing
// an option value from becoming a package or a persisted credential.
func parseInstallOptions(manager string, args []string, scope *installScope) installOptions {
	if manager == "docker-php-ext-configure" {
		return parsePHPConfigureOptions(args, scope)
	}
	result := installOptions{packages: make([]string, 0, len(args))}
	for index := 0; index < len(args); index++ {
		word := args[index]
		if !strings.HasPrefix(word, "-") {
			result.packages = append(result.packages, word)
			continue
		}
		name, attached, hasAttached := strings.Cut(word, "=")
		if manager == "docker-php-ext-install" && strings.HasPrefix(word, "-j") && len(word) > 2 {
			name, attached, hasAttached = "-j", word[2:], true
		}
		arity := installOptionArity(manager, name)
		if arity == unknownInstallOption {
			result.dropped = countCredentialValues(args, scope)
			result.reason = "unsupported_option"
			return result
		}
		if arity == flagInstallOption {
			if hasAttached {
				result.dropped = countCredentialValues(args, scope)
				result.reason = "unsupported_option"
				return result
			}
			if manager == "npm" && (name == "-g" || name == "--global") {
				result.global = true
			}
			continue
		}
		value := attached
		if !hasAttached {
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				result.reason = "missing_option_value"
				return result
			}
			value = args[index]
		}
		if sensitiveInstallValue("", value, scope) {
			result.dropped++
		}
	}
	return result
}

// parsePHPConfigureOptions follows the official helper's ext-name followed
// by configure flags form. Positional flag values are ambiguous and rejected.
func parsePHPConfigureOptions(args []string, scope *installScope) installOptions {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return installOptions{reason: "missing_package", dropped: countCredentialValues(args, scope)}
	}
	result := installOptions{packages: []string{args[0]}}
	for _, word := range args[1:] {
		name, value, hasValue := strings.Cut(word, "=")
		if !strings.HasPrefix(name, "--with-") && !strings.HasPrefix(name, "--without-") &&
			!strings.HasPrefix(name, "--enable-") && !strings.HasPrefix(name, "--disable-") {
			return installOptions{reason: "unsupported_option", dropped: countCredentialValues(args, scope)}
		}
		if hasValue && sensitiveInstallValue("", value, scope) {
			result.dropped++
		}
	}
	return result
}

func countCredentialValues(values []string, scope *installScope) int {
	count := 0
	for _, value := range values {
		if sensitiveInstallValue("", value, scope) {
			count++
		}
	}
	return count
}

func installOptionArity(manager, name string) int {
	switch manager {
	case "apt", "apt-get":
		switch name {
		case "-y", "--yes", "-q", "-qq", "--quiet", "--no-install-recommends", "--reinstall", "--only-upgrade":
			return flagInstallOption
		case "-o", "--option", "-t", "--target-release":
			return valueInstallOption
		}
	case "apk":
		switch name {
		case "--no-cache", "--update-cache", "-U", "--latest":
			return flagInstallOption
		case "--virtual", "-t", "--repository", "-X", "--keys-dir", "--root":
			return valueInstallOption
		}
	case "yum", "dnf":
		switch name {
		case "-y", "--assumeyes", "-q", "--quiet", "--best", "--allowerasing":
			return flagInstallOption
		case "--setopt", "--enablerepo", "--disablerepo", "--installroot", "--releasever":
			return valueInstallOption
		}
	case "pip", "pip3":
		switch name {
		case "-U", "--upgrade", "--no-cache-dir", "--no-deps", "--pre", "--user", "--quiet", "-q":
			return flagInstallOption
		case "--index-url", "-i", "--extra-index-url", "--trusted-host", "--find-links", "-f", "--requirement", "-r", "--constraint", "-c", "--target", "-t", "--prefix", "--root":
			return valueInstallOption
		}
	case "npm":
		switch name {
		case "-g", "--global", "--save", "--save-dev", "--no-save", "--ignore-scripts", "--force":
			return flagInstallOption
		case "--prefix", "--registry", "--cache":
			return valueInstallOption
		}
	case "pecl":
		switch name {
		case "-f", "--force", "--nobuild", "--nodeps":
			return flagInstallOption
		case "-D":
			return valueInstallOption
		}
	case "a2enmod":
		switch name {
		case "-q", "--quiet", "-f", "--force":
			return flagInstallOption
		}
	case "docker-php-ext-install":
		if name == "-j" {
			return valueInstallOption
		}
		if name == "--jobs" || name == "--ini-name" {
			return valueInstallOption
		}
	case "docker-php-ext-enable":
		if name == "--ini-name" {
			return valueInstallOption
		}
	}
	return unknownInstallOption
}
