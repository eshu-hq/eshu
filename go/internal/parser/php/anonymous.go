package php

import "fmt"

func phpAnonymousClassName(lineNumber int) string {
	return fmt.Sprintf("anonymous_class_%d", lineNumber)
}
