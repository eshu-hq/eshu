// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"sort"

	"golang.org/x/tools/go/packages"
)

// calledFunction resolves a call target to its declared function object. A
// callable value is resolved as a dynamic call so the caller can fail closed
// only when that boundary is reachable from a reducer port.
func calledFunction(info *types.Info, expression ast.Expr) (*types.Func, bool, error) {
	expression = ast.Unparen(expression)
	if typed, ok := info.Types[expression]; ok && typed.IsType() {
		return nil, false, nil
	}

	switch target := expression.(type) {
	case *ast.Ident:
		object := info.ObjectOf(target)
		if object == nil {
			return nil, false, fmt.Errorf("call target %s has no type object", target.Name)
		}
		if function, ok := object.(*types.Func); ok {
			return function, true, nil
		}
		_, callable := object.Type().Underlying().(*types.Signature)
		return nil, callable, nil
	case *ast.SelectorExpr:
		if selection := info.Selections[target]; selection != nil {
			if function, ok := selection.Obj().(*types.Func); ok {
				return function, true, nil
			}
			_, callable := selection.Obj().Type().Underlying().(*types.Signature)
			return nil, callable, nil
		}
		object := info.ObjectOf(target.Sel)
		if object == nil {
			return nil, false, fmt.Errorf("selector call target %s has no type object", target.Sel.Name)
		}
		if function, ok := object.(*types.Func); ok {
			return function, true, nil
		}
		_, callable := object.Type().Underlying().(*types.Signature)
		return nil, callable, nil
	case *ast.IndexExpr:
		return calledIndexedFunction(info, expression, target.X)
	case *ast.IndexListExpr:
		return calledIndexedFunction(info, expression, target.X)
	case *ast.FuncLit:
		return nil, false, nil
	}

	if typed, ok := info.Types[expression]; ok {
		_, callable := typed.Type.Underlying().(*types.Signature)
		return nil, callable, nil
	}
	return nil, false, fmt.Errorf("call target %T has no type information", expression)
}

func calledIndexedFunction(
	info *types.Info,
	expression ast.Expr,
	container ast.Expr,
) (*types.Func, bool, error) {
	function, resolved, err := calledFunction(info, container)
	if err != nil || resolved {
		return function, resolved, err
	}
	typed, ok := info.Types[expression]
	if !ok {
		return nil, false, nil
	}
	_, callable := typed.Type.Underlying().(*types.Signature)
	return nil, callable, nil
}

// callTargetObject returns the object that owns a static or dynamic call
// expression. For indexed callable containers, it returns the container object.
func callTargetObject(info *types.Info, expression ast.Expr) types.Object {
	switch target := ast.Unparen(expression).(type) {
	case *ast.Ident:
		return info.ObjectOf(target)
	case *ast.SelectorExpr:
		if selection := info.Selections[target]; selection != nil {
			return selection.Obj()
		}
		return info.ObjectOf(target.Sel)
	case *ast.IndexExpr:
		return callTargetObject(info, target.X)
	case *ast.IndexListExpr:
		return callTargetObject(info, target.X)
	case *ast.CallExpr:
		return callTargetObject(info, target.Fun)
	default:
		return nil
	}
}

// isPackageCallable reports whether object identifies a callable value owned
// by the scanned package. The one exception is the exact injected clock field:
// invoking it observes time and cannot reach package Cypher.
func isPackageCallable(pkg *packages.Package, object types.Object) bool {
	if object == nil || object.Pkg() == nil || object.Pkg().Path() != pkg.PkgPath {
		return false
	}
	if isOrphanSweepClockField(pkg, object) {
		return false
	}
	return typeContainsCallable(object.Type(), map[types.Type]struct{}{})
}

func typeContainsCallable(valueType types.Type, seen map[types.Type]struct{}) bool {
	if valueType == nil {
		return false
	}
	valueType = types.Unalias(valueType)
	if _, visited := seen[valueType]; visited {
		return false
	}
	seen[valueType] = struct{}{}
	switch typed := valueType.Underlying().(type) {
	case *types.Signature:
		return true
	case *types.Array:
		return typeContainsCallable(typed.Elem(), seen)
	case *types.Slice:
		return typeContainsCallable(typed.Elem(), seen)
	case *types.Map:
		return typeContainsCallable(typed.Elem(), seen)
	}
	return false
}

func isOrphanSweepClockField(pkg *packages.Package, object types.Object) bool {
	field, ok := object.(*types.Var)
	if !ok || !field.IsField() || field.Name() != "Now" {
		return false
	}
	typeName, ok := pkg.Types.Scope().Lookup("OrphanSweepStore").(*types.TypeName)
	if !ok {
		return false
	}
	structure, ok := types.Unalias(typeName.Type()).Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for index := 0; index < structure.NumFields(); index++ {
		if structure.Field(index) == field {
			return true
		}
	}
	return false
}

func dynamicCallEvidence(pkg *packages.Package, call *ast.CallExpr, object types.Object) string {
	position := pkg.Fset.Position(call.Fun.Pos())
	target := "callable expression"
	if object != nil {
		target = object.Name()
	}
	return fmt.Sprintf(
		"%s:%d: unresolved package-local call to %s",
		filepath.Base(position.Filename),
		position.Line,
		target,
	)
}

// typedFunctionKey renders a function object as "Receiver.Method" or "Func".
func typedFunctionKey(function *types.Func) (string, error) {
	signature, ok := function.Type().(*types.Signature)
	if !ok {
		return "", fmt.Errorf("%s does not have a function signature", function.FullName())
	}
	if signature.Recv() == nil {
		return function.Name(), nil
	}
	receiver := types.Unalias(signature.Recv().Type())
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = types.Unalias(pointer.Elem())
	}
	named, ok := receiver.(*types.Named)
	if !ok || named.Obj() == nil {
		return "", fmt.Errorf("%s has unsupported receiver type %s", function.FullName(), receiver)
	}
	return named.Obj().Name() + "." + function.Name(), nil
}

func isInterfaceMethod(function *types.Func) bool {
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return false
	}
	_, ok = types.Unalias(signature.Recv().Type()).Underlying().(*types.Interface)
	return ok
}

func interfaceMethodImplementations(
	method *types.Func,
	keysByObject map[*types.Func]string,
) []string {
	signature := method.Type().(*types.Signature)
	interfaceType, ok := types.Unalias(signature.Recv().Type()).Underlying().(*types.Interface)
	if !ok {
		return nil
	}
	interfaceType.Complete()
	var implementations []string
	for candidate, key := range keysByObject {
		candidateSignature := candidate.Type().(*types.Signature)
		if candidate.Name() != method.Name() || candidateSignature.Recv() == nil {
			continue
		}
		if types.Implements(candidateSignature.Recv().Type(), interfaceType) {
			implementations = appendUnique(implementations, key)
		}
	}
	sort.Strings(implementations)
	return implementations
}

func appendUnique(values []string, candidate string) []string {
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func mergeUnique(values []string, candidates []string) []string {
	for _, candidate := range candidates {
		values = appendUnique(values, candidate)
	}
	sort.Strings(values)
	return values
}
