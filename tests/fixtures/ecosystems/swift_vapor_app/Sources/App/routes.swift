import Vapor

// POSITIVE (#5337 swift.vapor_route_handler): `import Vapor` is present and
// `use: healthCheck` binds healthCheck as a real HTTP route handler, so the
// parser marks healthCheck as a vapor_route_handler dead-code root. Nothing in
// this repo calls healthCheck directly, so the root is what keeps it out of the
// dead-code candidate buckets — the golden gate asserts exactly that.
func routes(_ app: Application) throws {
    app.get("health", use: healthCheck)
}

func healthCheck(_ req: Request) async throws -> String {
    "ok"
}
