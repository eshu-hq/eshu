// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// App bootstrap shape informed by public patterns in Vapor-style server
// apps (a top-level configure(_:) entrypoint registering route
// collections). Synthetic content, hand-authored for this fixture.

import Vapor

public func configure(_ app: Application) throws {
    try routes(app)
}

func routes(_ app: Application) throws {
    try app.register(collection: UserController())

    app.get("health") { req -> String in
        return "ok"
    }
}
