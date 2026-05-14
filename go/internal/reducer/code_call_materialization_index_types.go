package reducer

type codeEntityIndex struct {
	entitiesByPathLine          map[string]string
	spansByPath                 map[string][]codeFunctionSpan
	containersByPath            map[string][]codeFunctionSpan
	uniqueNameByPath            map[string]map[string]string
	uniqueNameByRepo            map[string]map[string]string
	uniqueNameByRepoDir         map[string]map[string]map[string]string
	constructorByPath           map[string]map[string]string
	goMethodReturnTypes         map[string]map[string]string
	entityFileByID              map[string]string
	entityTypeByID              map[string]string
	javaScriptAliasesByEntityID map[string]javaScriptStaticAliasSet
}

type codeFunctionSpan struct {
	startLine int
	endLine   int
	entityID  string
	names     []string
}
