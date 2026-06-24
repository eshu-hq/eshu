// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

type typeScriptIndexCandidates struct {
	classMethods          map[string]map[string]map[string]map[string]struct{}
	interfaces            map[string]map[string]struct{}
	interfaceImplementers map[string]map[string]map[string]struct{}
}

func newTypeScriptIndexCandidates() typeScriptIndexCandidates {
	return typeScriptIndexCandidates{
		classMethods:          make(map[string]map[string]map[string]map[string]struct{}),
		interfaces:            make(map[string]map[string]struct{}),
		interfaceImplementers: make(map[string]map[string]map[string]struct{}),
	}
}

func (c typeScriptIndexCandidates) addFunction(repositoryID string, item map[string]any, entityID string) {
	addTypeScriptClassMethodCandidate(c.classMethods, repositoryID, item, entityID)
}

func (c typeScriptIndexCandidates) addType(bucket string, repositoryID string, item map[string]any) {
	switch bucket {
	case "classes":
		addTypeScriptInterfaceImplementer(c.interfaceImplementers, repositoryID, item)
	case "interfaces":
		addTypeScriptInterfaceDeclaration(c.interfaces, repositoryID, item)
	}
}

func (c typeScriptIndexCandidates) uniqueMethods() map[string]map[string]map[string]string {
	return uniqueTypeScriptInterfaceMethods(c.interfaces, c.interfaceImplementers, c.classMethods)
}

func addTypeScriptClassMethodCandidate(
	candidates map[string]map[string]map[string]map[string]struct{},
	repositoryID string,
	item map[string]any,
	entityID string,
) {
	if repositoryID == "" || entityID == "" || !codeCallTypeScriptEntity(item) {
		return
	}
	className := strings.TrimSpace(anyToString(item["class_context"]))
	methodName := strings.TrimSpace(anyToString(item["name"]))
	if className == "" || methodName == "" {
		return
	}
	if _, ok := candidates[repositoryID]; !ok {
		candidates[repositoryID] = make(map[string]map[string]map[string]struct{})
	}
	if _, ok := candidates[repositoryID][className]; !ok {
		candidates[repositoryID][className] = make(map[string]map[string]struct{})
	}
	if _, ok := candidates[repositoryID][className][methodName]; !ok {
		candidates[repositoryID][className][methodName] = make(map[string]struct{})
	}
	candidates[repositoryID][className][methodName][entityID] = struct{}{}
}

func addTypeScriptInterfaceImplementer(
	implementers map[string]map[string]map[string]struct{},
	repositoryID string,
	item map[string]any,
) {
	if repositoryID == "" || !codeCallTypeScriptEntity(item) {
		return
	}
	className := strings.TrimSpace(anyToString(item["name"]))
	if className == "" {
		return
	}
	interfaces := codeCallMetadataStringSlice(item, "implemented_interfaces")
	if len(interfaces) == 0 {
		return
	}
	if _, ok := implementers[repositoryID]; !ok {
		implementers[repositoryID] = make(map[string]map[string]struct{})
	}
	for _, iface := range interfaces {
		iface = strings.TrimSpace(iface)
		if iface == "" {
			continue
		}
		if _, ok := implementers[repositoryID][iface]; !ok {
			implementers[repositoryID][iface] = make(map[string]struct{})
		}
		implementers[repositoryID][iface][className] = struct{}{}
	}
}

func addTypeScriptInterfaceDeclaration(
	interfaces map[string]map[string]struct{},
	repositoryID string,
	item map[string]any,
) {
	if repositoryID == "" || !codeCallTypeScriptEntity(item) {
		return
	}
	interfaceName := strings.TrimSpace(anyToString(item["name"]))
	if interfaceName == "" {
		return
	}
	if _, ok := interfaces[repositoryID]; !ok {
		interfaces[repositoryID] = make(map[string]struct{})
	}
	interfaces[repositoryID][interfaceName] = struct{}{}
}

func uniqueTypeScriptInterfaceMethods(
	declaredInterfaces map[string]map[string]struct{},
	implementers map[string]map[string]map[string]struct{},
	classMethods map[string]map[string]map[string]map[string]struct{},
) map[string]map[string]map[string]string {
	unique := make(map[string]map[string]map[string]string)
	for repositoryID, interfaces := range implementers {
		for interfaceName, classes := range interfaces {
			if _, ok := declaredInterfaces[repositoryID][interfaceName]; !ok {
				continue
			}
			methodCandidates := make(map[string]map[string]struct{})
			for className := range classes {
				for methodName, entityIDs := range classMethods[repositoryID][className] {
					if _, ok := methodCandidates[methodName]; !ok {
						methodCandidates[methodName] = make(map[string]struct{})
					}
					for entityID := range entityIDs {
						methodCandidates[methodName][entityID] = struct{}{}
					}
				}
			}
			for methodName, entityIDs := range methodCandidates {
				if len(entityIDs) != 1 {
					continue
				}
				for entityID := range entityIDs {
					if _, ok := unique[repositoryID]; !ok {
						unique[repositoryID] = make(map[string]map[string]string)
					}
					if _, ok := unique[repositoryID][interfaceName]; !ok {
						unique[repositoryID][interfaceName] = make(map[string]string)
					}
					unique[repositoryID][interfaceName][methodName] = entityID
				}
			}
		}
	}
	return unique
}

func codeCallTypeScriptEntity(item map[string]any) bool {
	switch strings.TrimSpace(anyToString(item["lang"])) {
	case "typescript", "tsx":
		return true
	default:
		return false
	}
}
