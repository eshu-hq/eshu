package golang

import (
	"unicode"
	"unicode/utf8"
)

func goIdentifierIsExported(name string) bool {
	first, _ := utf8.DecodeRuneInString(name)
	return first != utf8.RuneError && unicode.IsUpper(first)
}
