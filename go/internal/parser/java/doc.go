// Package java extracts Java-owned parser evidence that can stay independent
// from the parent parser dispatch package.
//
// Callers use MetadataClassReferences to turn bounded ServiceLoader and Spring
// metadata files into ClassReference rows. The package rejects unsupported
// paths, invalid class names, and duplicate class names so the parent parser
// does not emit reachability evidence that the metadata file did not prove.
package java
