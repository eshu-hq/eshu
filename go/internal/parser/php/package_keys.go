// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/php/composer"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// This file stamps the PHP half of the #7601 cross-repository call keys. A
// producer class or interface that composer autoloads through psr-4 carries
// package_id (its composer.json name) and export_name (its fully qualified
// name), and a public static method of such a class carries export_name
// <FQCN>::<method>. A consumer's `new X(` or `X::method(` whose class is named by
// a `use` import or a fully qualified literal carries package_export_symbol
// package:<locked package>#<FQCN>[::<method>]. The reducer derives the same
// key from the producer side and resolves a call only when exactly one
// definition carries it, so each rule here leaves a key off rather than name a
// symbol it cannot prove.

// phpPackageKeys holds the per-file state of the #7601 key rules.
type phpPackageKeys struct {
	repoRoot string
	path     string
	// useTargets maps each local class name a `use` import binds (the alias, or
	// the last segment) to the fully qualified name. It is built from the
	// import rows themselves because importAliases keeps only the last
	// segment of an aliased target, which loses the namespace.
	useTargets map[string]string
	// blockedUse holds local names bound to two different classes.
	blockedUse map[string]struct{}
	// types are the class and interface declarations eligible for a producer
	// key, in declaration order. Anonymous classes are never recorded.
	types []phpTypeDeclaration
	// publicStaticMethods are the class methods eligible for a producer key.
	publicStaticMethods []phpStaticMethod
	// locks is the file's composer.lock chain, read once at the first
	// candidate call.
	locks     composer.Locks
	locksRead bool
	// lockedPackages memoizes locks.Package per fully qualified class name.
	lockedPackages map[string]string
}

// phpTypeDeclaration is one named class or interface declaration.
type phpTypeDeclaration struct {
	name    string
	isClass bool
	item    map[string]any
}

// phpStaticMethod is one public static method of a class declaration.
type phpStaticMethod struct {
	className string
	item      map[string]any
}

// newPHPPackageKeys returns the key state for one file, or nil when the file
// declares more than one namespace. Every method is a no-op on nil.
func newPHPPackageKeys(repoRoot string, path string, root *tree_sitter.Node) *phpPackageKeys {
	if phpNamespaceCount(root) > 1 {
		return nil
	}
	return &phpPackageKeys{
		repoRoot:       repoRoot,
		path:           path,
		useTargets:     make(map[string]string),
		blockedUse:     make(map[string]struct{}),
		lockedPackages: make(map[string]string),
	}
}

// recordUse binds the local name of one `use` class import to its fully
// qualified name. A local name bound to two different classes is blocked.
func (k *phpPackageKeys) recordUse(fqcn string, alias string) {
	if k == nil {
		return
	}
	fqcn = strings.Trim(strings.TrimSpace(fqcn), `\`)
	local := strings.TrimSpace(alias)
	if local == "" {
		local = shared.LastPathSegment(fqcn, `\`)
	}
	if !phpIsQualifiedName(fqcn) || !phpIsPlainName(local) {
		return
	}
	if existing, ok := k.useTargets[local]; ok && existing != fqcn {
		k.blockedUse[local] = struct{}{}
		return
	}
	k.useTargets[local] = fqcn
}

// recordType stages a named class or interface declaration for the producer
// key.
func (k *phpPackageKeys) recordType(name string, isClass bool, item map[string]any) {
	if k == nil {
		return
	}
	k.types = append(k.types, phpTypeDeclaration{name: name, isClass: isClass, item: item})
}

// recordMethod stages a class method for the producer key when it is public
// and static, the only methods another package can call as Class::method().
func (k *phpPackageKeys) recordMethod(node *tree_sitter.Node, typeName string, typeKind string, isPublic bool, item map[string]any) {
	if k == nil {
		return
	}
	if typeKind != "class_declaration" || typeName == "" || !isPublic || !phpMethodIsStatic(node) {
		return
	}
	k.publicStaticMethods = append(k.publicStaticMethods, phpStaticMethod{className: typeName, item: item})
}

// stampProducers sets package_id and export_name on the file's classes,
// interfaces, and public static class methods that composer autoloads for a
// consumer. namespace is the file's single namespace ("" for the global one).
func (k *phpPackageKeys) stampProducers(namespace string) {
	if k == nil {
		return
	}
	keyedClasses := map[string][2]string{}
	for _, declared := range k.types {
		if !phpIsPlainName(declared.name) {
			continue
		}
		fqcn := phpQualify(namespace, declared.name)
		packageName := composer.ProducerPackage(k.repoRoot, k.path, fqcn)
		if packageName == "" {
			continue
		}
		declared.item["package_id"] = packageName
		declared.item["export_name"] = fqcn
		if declared.isClass {
			keyedClasses[declared.name] = [2]string{packageName, fqcn}
		}
	}
	for _, method := range k.publicStaticMethods {
		keyed, ok := keyedClasses[method.className]
		name, _ := method.item["name"].(string)
		if !ok || !phpIsPlainName(name) {
			continue
		}
		method.item["package_id"] = keyed[0]
		method.item["export_name"] = keyed[1] + "::" + name
	}
}

// classTarget returns the fully qualified class a `new` or static-call class
// node names, or "" when the node is not a bare imported name or a fully
// qualified literal. A relative qualified name (Sub\X, namespace\X), self,
// static, parent, and a variable class are never keyed.
func (k *phpPackageKeys) classTarget(node *tree_sitter.Node, source []byte) string {
	if k == nil || node == nil {
		return ""
	}
	text := strings.TrimSpace(shared.NodeText(node, source))
	switch node.Kind() {
	case "name":
		if _, blocked := k.blockedUse[text]; blocked {
			return ""
		}
		return k.useTargets[text]
	case "qualified_name":
		if fqcn, ok := strings.CutPrefix(text, `\`); ok && phpIsQualifiedName(fqcn) {
			return fqcn
		}
	}
	return ""
}

// consumerKey returns the package_export_symbol for a call to member of fqcn
// ("" member for `new`), or "" when no composer.lock names the package that
// provides fqcn.
func (k *phpPackageKeys) consumerKey(fqcn string, member string) string {
	if k == nil || fqcn == "" {
		return ""
	}
	packageName := k.lockedPackage(fqcn)
	if packageName == "" {
		return ""
	}
	key := "package:" + packageName + "#" + fqcn
	if member != "" {
		key += "::" + member
	}
	return key
}

// hasLocks reports whether the file's composer.lock chain names any package,
// reading the chain once.
func (k *phpPackageKeys) hasLocks() bool {
	if !k.locksRead {
		k.locksRead = true
		k.locks = composer.LocksFor(k.repoRoot, k.path)
	}
	return !k.locks.Empty()
}

// lockedPackage returns the locked composer package that provides the class
// or namespaced function fqcn, memoized per name, or "" when none does.
func (k *phpPackageKeys) lockedPackage(fqcn string) string {
	if fqcn == "" || !k.hasLocks() {
		return ""
	}
	packageName, seen := k.lockedPackages[fqcn]
	if !seen {
		packageName = k.locks.Package(fqcn)
		k.lockedPackages[fqcn] = packageName
	}
	return packageName
}

// phpNamespaceCount returns how many namespace declarations the file has. A
// file with more than one gets no keys: its use imports and class names
// belong to different blocks, and composer maps one class per file anyway.
func phpNamespaceCount(root *tree_sitter.Node) int {
	count := 0
	cursor := root.Walk()
	defer cursor.Close()
	for _, child := range root.NamedChildren(cursor) {
		if child.Kind() == "namespace_definition" {
			count++
		}
	}
	return count
}

// phpMethodIsStatic reports whether a method declaration carries the static
// modifier.
func phpMethodIsStatic(node *tree_sitter.Node) bool {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		if child.Kind() == "static_modifier" {
			return true
		}
	}
	return false
}

func phpQualify(namespace string, name string) string {
	namespace = strings.Trim(strings.TrimSpace(namespace), `\`)
	if namespace == "" {
		return name
	}
	return namespace + `\` + name
}

// phpIsPlainName reports whether name is one PHP identifier: a letter, '_',
// or a byte >= 0x80, then those or digits.
func phpIsPlainName(name string) bool {
	if name == "" {
		return false
	}
	for index := 0; index < len(name); index++ {
		c := name[index]
		switch {
		case c == '_' || c >= 0x80 || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
		case index > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// phpIsQualifiedName reports whether name is identifiers joined by '\'.
func phpIsQualifiedName(name string) bool {
	for _, segment := range strings.Split(name, `\`) {
		if !phpIsPlainName(segment) {
			return false
		}
	}
	return name != ""
}
