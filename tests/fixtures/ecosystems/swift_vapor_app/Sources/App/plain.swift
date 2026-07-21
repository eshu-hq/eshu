// FOIL (#5337): byte-for-byte the same `use:` argument-label shape as a Vapor
// route registration, but this file does NOT `import Vapor`. Because the
// vapor_route_handler root is gated on a per-file `import Vapor`, statusReport
// must NOT be rooted. It is an ordinary builder-DSL `use:` call. statusReport is
// unreferenced, so with no root it must surface as a dead-code candidate — the
// discriminating opposite of healthCheck in routes.swift.
func configure(_ builder: Builder) {
    builder.middleware(use: statusReport)
}

func statusReport(_ ctx: Context) -> String {
    "status"
}
