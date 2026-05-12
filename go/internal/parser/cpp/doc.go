// Package cpp parses C++ source files into Eshu parser payloads.
//
// The package emits syntax facts for C++ functions, types, includes, macros,
// aliases, and calls. It also annotates derived dead-code root metadata for
// direct parser evidence such as main functions, directly included local header
// declarations, virtual and override methods, direct callback arguments, and
// direct function-pointer initializer targets. Qualified out-of-class method
// definitions are keyed by the rightmost class qualifier so direct local header
// declarations can match namespace-scoped implementations. The package also
// recognizes bounded Node native-addon entrypoint macros. It does not claim
// exact C++ reachability because broader macro expansion, build-target
// selection, template instantiation, overload resolution, and broad dynamic
// dispatch are outside this package boundary.
package cpp
