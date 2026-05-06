package query

import "fmt"

func buildEntityStory(result map[string]any) string {
	if result == nil {
		return ""
	}

	summary := StringVal(result, "semantic_summary")
	if summary == "" {
		return ""
	}

	filePath := StringVal(result, "file_path")
	language := StringVal(result, "language")
	switch {
	case filePath != "" && language != "":
		return fmt.Sprintf("%s Defined in %s (%s).", summary, filePath, language)
	case filePath != "":
		return fmt.Sprintf("%s Defined in %s.", summary, filePath)
	case language != "":
		return fmt.Sprintf("%s Language: %s.", summary, language)
	default:
		return summary
	}
}
