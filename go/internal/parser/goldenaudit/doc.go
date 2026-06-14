// Package goldenaudit compares parser and reducer graph observations against
// independent source-language golden fixtures.
//
// Golden fixtures must describe expected nodes and edges directly rather than
// copying Eshu output back into the expected file. The package reports missing,
// unexpected, and duplicate graph facts deterministically so language-depth
// work can prove accuracy before promoting support claims.
package goldenaudit
