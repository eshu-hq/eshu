// Package runtimebind self-registers the Amazon OpenSearch Serverless
// metadata-only scanner with the AWS collector runtime.
//
// Importing this package for its side effect (a blank import in
// awsruntime/bindings) makes the opensearchserverless service_kind discoverable
// to the ingester without the runtime importing the scanner or its SDK adapter
// directly. The registration wires the SDK-backed client into the scanner under
// the ServiceOpenSearchServerless service kind.
package runtimebind
