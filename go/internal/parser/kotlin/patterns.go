package kotlin

import "regexp"

var (
	kotlinImportPattern        = regexp.MustCompile(`^\s*import\s+([^\s]+)(?:\s+as\s+([A-Za-z_]\w*))?`)
	kotlinClassPattern         = regexp.MustCompile(`^\s*(?:data\s+|sealed\s+|abstract\s+|open\s+)?class\s+([A-Za-z_]\w*)`)
	kotlinObjectPattern        = regexp.MustCompile(`^\s*object\s+([A-Za-z_]\w*)`)
	kotlinCompanionPattern     = regexp.MustCompile(`^\s*companion\s+object(?:\s+([A-Za-z_]\w*))?`)
	kotlinInterfacePattern     = regexp.MustCompile(`^\s*interface\s+([A-Za-z_]\w*)`)
	kotlinEnumPattern          = regexp.MustCompile(`^\s*enum\s+class\s+([A-Za-z_]\w*)`)
	kotlinFunctionPattern      = regexp.MustCompile(`\bfun\s+(?:<[^>]+>\s*)?(?:([A-Za-z_]\w*)\.)?([A-Za-z_]\w*)\s*\(`)
	kotlinConstructorPattern   = regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal)\s+)?constructor\s*\(`)
	kotlinVariablePattern      = regexp.MustCompile(`^\s*(?:private|public|protected|internal)?\s*(?:const\s+)?(?:val|var)\s+([A-Za-z_]\w*)`)
	kotlinTypedVariablePattern = regexp.MustCompile(`^\s*(?:private|public|protected|internal)?\s*(?:const\s+)?(?:val|var)\s+([A-Za-z_]\w*)\s*:\s*([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*(?:<[^>]+>)?\??)`)
	kotlinCtorAssignPattern    = regexp.MustCompile(`^\s*(?:val|var)\s+([A-Za-z_]\w*)\s*=\s*([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*(?:<[^>]+>)?\??)\s*\([^()]*\)\s*$`)
	kotlinStringAssignPattern  = regexp.MustCompile(`^\s*(?:val|var)\s+([A-Za-z_]\w*)\s*=\s*"([^"]*)"`)
	kotlinAliasAssignPattern   = regexp.MustCompile(`^\s*(?:val|var)\s+([A-Za-z_]\w*)\s*=\s*((?:this\.)?(?:[A-Za-z_]\w*(?:\([^)]*\))?)(?:\.(?:[A-Za-z_]\w*(?:\([^)]*\))?))*)\s*$`)
	kotlinThisCallPattern      = regexp.MustCompile(`this\.([A-Za-z_]\w*)\s*\(`)
	kotlinCallPattern          = regexp.MustCompile(`\b((?:[A-Za-z_]\w*(?:\([^)]*\))?)(?:\.(?:[A-Za-z_]\w*(?:\([^)]*\))?))*)\.([A-Za-z_]\w*)\s*\(`)
	kotlinInfixCallPattern     = regexp.MustCompile(`^(?:return\s+)?([A-Za-z_]\w*)\s+([A-Za-z_]\w*)\s+(.+)$`)
	kotlinAnnotationPattern    = regexp.MustCompile(`@([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*)`)
)
