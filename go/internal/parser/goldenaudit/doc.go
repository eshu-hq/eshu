// Package goldenaudit compares parser and reducer graph observations against
// independent source-language golden fixtures.
//
// Golden fixtures must describe expected nodes and edges directly rather than
// copying Eshu output back into the expected file. The package reports missing,
// unexpected, and duplicate graph facts deterministically so language-depth
// work can prove accuracy before promoting support claims.
//
// CompareGraph reports exact node and edge drift. ScoreAccuracy adds
// precision/recall of observed edges against golden edges, per relationship
// type and overall, with a wrong-target vs missing vs extra breakdown so a
// resolver that points an edge at the wrong target is caught even when its tier
// distribution looks healthy. Both are measurement only; neither parses source
// nor writes graph rows.
package goldenaudit
