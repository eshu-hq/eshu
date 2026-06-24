// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// receiverMethodCandidates accumulates, per repository, the set of entity ids
// that declare a given method on a given receiver type. It backs cross-repo
// receiver-typed call resolution for languages whose parser emits an inferred
// receiver type (inferred_obj_type) plus a class_context on the declaration but
// no dotted-import-to-file mapping (Swift modules, JavaScript classes). A method
// resolves only when exactly one entity declares it for the receiver type, so an
// ambiguous receiver never becomes graph truth.
type receiverMethodCandidates map[string]map[string]map[string]map[string]struct{}

// newReceiverMethodCandidates returns an empty accumulator.
func newReceiverMethodCandidates() receiverMethodCandidates {
	return make(receiverMethodCandidates)
}

// add records that entityID declares methodName on receiverType (taken from the
// declaration's class_context) within repositoryID. Items lacking a repository,
// entity id, class context, method name, or whose language is not a configured
// receiver-method language are ignored.
func (c receiverMethodCandidates) add(repositoryID string, item map[string]any, entityID string) {
	if repositoryID == "" || entityID == "" {
		return
	}
	if !receiverMethodLanguage(anyToString(item["lang"])) {
		return
	}
	receiverType := strings.TrimSpace(anyToString(item["class_context"]))
	methodName := strings.TrimSpace(anyToString(item["name"]))
	if receiverType == "" || methodName == "" {
		return
	}
	if _, ok := c[repositoryID]; !ok {
		c[repositoryID] = make(map[string]map[string]map[string]struct{})
	}
	if _, ok := c[repositoryID][receiverType]; !ok {
		c[repositoryID][receiverType] = make(map[string]map[string]struct{})
	}
	if _, ok := c[repositoryID][receiverType][methodName]; !ok {
		c[repositoryID][receiverType][methodName] = make(map[string]struct{})
	}
	c[repositoryID][receiverType][methodName][entityID] = struct{}{}
}

// unique collapses the accumulator to repositoryID -> receiverType -> methodName
// -> entityID, keeping only methods declared by exactly one entity for the
// receiver type. Ambiguous methods are dropped so the resolver returns nothing
// rather than inventing an edge.
func (c receiverMethodCandidates) unique() map[string]map[string]map[string]string {
	unique := make(map[string]map[string]map[string]string, len(c))
	for repositoryID, receivers := range c {
		for receiverType, methods := range receivers {
			for methodName, entityIDs := range methods {
				if len(entityIDs) != 1 {
					continue
				}
				for entityID := range entityIDs {
					if _, ok := unique[repositoryID]; !ok {
						unique[repositoryID] = make(map[string]map[string]string)
					}
					if _, ok := unique[repositoryID][receiverType]; !ok {
						unique[repositoryID][receiverType] = make(map[string]string)
					}
					unique[repositoryID][receiverType][methodName] = entityID
				}
			}
		}
	}
	return unique
}

// receiverMethodLanguage reports whether a parser language uses the shared
// receiver-method index. These languages emit an inferred receiver type but have
// no dotted-import-to-file mapping, so resolution is repo-scoped type inference
// rather than import binding.
func receiverMethodLanguage(lang string) bool {
	switch strings.TrimSpace(lang) {
	case "swift", "javascript", "jsx":
		return true
	default:
		return false
	}
}

// resolveReceiverMethodCallee resolves a receiver-typed call to the unique entity
// declaring the called method on the inferred receiver type within the caller's
// repository. It returns the entity id, its file, and type-inference provenance,
// or empty values when the receiver type, method, or unique declaration is
// absent. Callers register it in the before-repo-fallback phase so a confident
// type-inferred match wins over the broad repo-unique-name guess.
func resolveReceiverMethodCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	receiverType := strings.TrimSpace(anyToString(ctx.call["inferred_obj_type"]))
	methodName := ctx.callName()
	if ctx.repositoryID == "" || receiverType == "" || methodName == "" {
		return "", "", ""
	}
	entityID := ctx.index.receiverMethodsByRepo[ctx.repositoryID][receiverType][methodName]
	if entityID == "" {
		return "", "", ""
	}
	return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodTypeInferred
}
