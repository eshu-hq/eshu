// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package composer reads the Composer manifests the PHP adapter needs to key
// cross-repository calls (#7601): the nearest composer.json for the producer
// half (ProducerPackage, ManifestName) and every composer.lock up to the
// repository root for the consumer half (LockedPackage, HasLock).
//
// A producer class is keyed only when composer could autoload it for a
// consumer: the nearest composer.json has a name in the composer grammar
// (ValidPackageName) and an autoload psr-4 prefix maps the class to exactly
// the file that declares it. A consumer class is keyed only when a
// composer.lock names the package that provides it through the longest
// covering psr-4 prefix. Both rules return "" whenever the answer is not
// proven, so the reducer's unique-or-unresolved rule never sees a guessed key.
//
// Parsed manifests and locks are memoized by (path, stat) in the same bounded
// cache the JavaScript adapter uses for package.json. This package is a leaf:
// it must not import the parent php package or the parser package.
package composer
