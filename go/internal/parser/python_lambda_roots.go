package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type pythonLambdaHandlerSet map[string]map[string]struct{}

func (handlers pythonLambdaHandlerSet) Has(path string, functionName string) bool {
	functionName = strings.TrimSpace(functionName)
	if functionName == "" {
		return false
	}
	functions := handlers[filepath.Clean(path)]
	if len(functions) == 0 {
		return false
	}
	_, ok := functions[functionName]
	return ok
}

func pythonLambdaHandlerRoots(repoRoot string, sourcePath string) pythonLambdaHandlerSet {
	handlers := make(pythonLambdaHandlerSet)
	repoRoot = pythonLambdaRepoRoot(repoRoot, sourcePath)
	for _, configPath := range pythonLambdaConfigCandidates(repoRoot, sourcePath) {
		source, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}
		documents, err := decodeYAMLDocuments(sanitizeYAMLTemplating(string(source)))
		if err != nil {
			continue
		}
		for _, document := range documents {
			object, ok := document.(map[string]any)
			if !ok {
				continue
			}
			pythonLambdaAddSAMHandlers(handlers, filepath.Dir(configPath), object)
			pythonLambdaAddServerlessHandlers(handlers, filepath.Dir(configPath), object)
		}
	}
	return handlers
}

func pythonLambdaRepoRoot(repoRoot string, sourcePath string) string {
	if strings.TrimSpace(repoRoot) != "" {
		return filepath.Clean(repoRoot)
	}
	return filepath.Dir(sourcePath)
}

func pythonLambdaConfigCandidates(repoRoot string, sourcePath string) []string {
	names := []string{"template.yaml", "template.yml", "serverless.yaml", "serverless.yml"}
	seen := make(map[string]struct{})
	var candidates []string
	dir := filepath.Dir(sourcePath)
	for {
		for _, name := range names {
			candidate := filepath.Join(dir, name)
			if _, ok := seen[candidate]; !ok {
				seen[candidate] = struct{}{}
				candidates = append(candidates, candidate)
			}
		}
		if filepath.Clean(dir) == filepath.Clean(repoRoot) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return candidates
}

func pythonLambdaAddSAMHandlers(
	handlers pythonLambdaHandlerSet,
	templateDir string,
	document map[string]any,
) {
	resources, _ := document["Resources"].(map[string]any)
	for _, rawResource := range resources {
		resource, _ := rawResource.(map[string]any)
		resourceType, _ := resource["Type"].(string)
		if resourceType != "AWS::Serverless::Function" && resourceType != "AWS::Lambda::Function" {
			continue
		}
		properties, _ := resource["Properties"].(map[string]any)
		if !pythonLambdaRuntimeIsPython(properties["Runtime"]) {
			continue
		}
		handler, _ := properties["Handler"].(string)
		codeURI := pythonLambdaCodeURI(properties["CodeUri"])
		pythonLambdaAddHandler(handlers, templateDir, codeURI, handler)
	}
}

func pythonLambdaAddServerlessHandlers(
	handlers pythonLambdaHandlerSet,
	configDir string,
	document map[string]any,
) {
	provider, _ := document["provider"].(map[string]any)
	providerRuntime := provider["runtime"]
	functions, _ := document["functions"].(map[string]any)
	for _, rawFunction := range functions {
		function, _ := rawFunction.(map[string]any)
		runtime := function["runtime"]
		if runtime == nil {
			runtime = providerRuntime
		}
		if !pythonLambdaRuntimeIsPython(runtime) {
			continue
		}
		handler, _ := function["handler"].(string)
		pythonLambdaAddHandler(handlers, configDir, ".", handler)
	}
}

func pythonLambdaRuntimeIsPython(raw any) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(fmt.Sprint(raw))), "python")
}

func pythonLambdaCodeURI(raw any) string {
	value := strings.TrimSpace(fmt.Sprint(raw))
	if value == "<nil>" {
		value = ""
	}
	if value == "" {
		return "."
	}
	return value
}

func pythonLambdaAddHandler(
	handlers pythonLambdaHandlerSet,
	baseDir string,
	codeURI string,
	handler string,
) {
	handler = strings.TrimSpace(handler)
	lastDot := strings.LastIndex(handler, ".")
	if lastDot < 0 {
		return
	}
	modulePath := handler[:lastDot]
	functionName := handler[lastDot+1:]
	if strings.TrimSpace(modulePath) == "" || strings.TrimSpace(functionName) == "" {
		return
	}
	if strings.Contains(codeURI, "://") {
		return
	}
	sourcePath := filepath.Join(baseDir, filepath.Clean(codeURI), strings.ReplaceAll(modulePath, ".", string(filepath.Separator))+".py")
	sourcePath = filepath.Clean(sourcePath)
	if handlers[sourcePath] == nil {
		handlers[sourcePath] = make(map[string]struct{})
	}
	handlers[sourcePath][strings.TrimSpace(functionName)] = struct{}{}
}
