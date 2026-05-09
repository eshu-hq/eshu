package python

import (
	"fmt"
	"os"
)

func convertNotebookToTempPython(path string, source []byte) (string, error) {
	code, err := NotebookSource(source)
	if err != nil {
		return "", fmt.Errorf("convert notebook %q: %w", path, err)
	}

	tempFile, err := os.CreateTemp("", "eshu-notebook-*.py")
	if err != nil {
		return "", fmt.Errorf("create temporary python file for %q: %w", path, err)
	}
	defer func() {
		_ = tempFile.Close()
	}()

	if _, err := tempFile.WriteString(code); err != nil {
		_ = os.Remove(tempFile.Name())
		return "", fmt.Errorf("write temporary python file for %q: %w", path, err)
	}
	return tempFile.Name(), nil
}
