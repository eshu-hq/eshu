// Package extensionconformance validates optional component fixtures against
// the manifest and collector SDK contracts.
//
// The package is read-only: it loads a component manifest, derives the
// host-declared SDK result contract, validates operator-supplied fixture
// results, and reports whether findings block publication or hosted
// activation. It does not install components, claim workflow work, write graph
// truth, or run Compose services.
package extensionconformance
