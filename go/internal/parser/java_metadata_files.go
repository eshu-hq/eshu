package parser

import (
	javaparser "github.com/eshu-hq/eshu/go/internal/parser/java"
)

func parseJavaMetadata(path string, isDependency bool) (map[string]any, error) {
	return javaparser.ParseMetadata(path, isDependency)
}
