package python

import "testing"

func TestNotebookSourceKeepsOnlyCodeCells(t *testing.T) {
	t.Parallel()

	source := []byte(`{
  "cells": [
    {"cell_type": "markdown", "source": ["# Title\n"]},
    {"cell_type": "code", "source": ["import os\n", "\n", "def hello():\n", "    return os.getcwd()\n"]},
    {"cell_type": "code", "source": "value = 1\n"}
  ]
}`)

	got, err := NotebookSource(source)
	if err != nil {
		t.Fatalf("NotebookSource() error = %v, want nil", err)
	}
	want := "import os\n\ndef hello():\n    return os.getcwd()\n\n\nvalue = 1\n"
	if got != want {
		t.Fatalf("NotebookSource() = %q, want %q", got, want)
	}
}

func TestNotebookSourceRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	if _, err := NotebookSource([]byte(`{"cells": [`)); err == nil {
		t.Fatalf("NotebookSource() error = nil, want invalid JSON error")
	}
}
