// Widget route collection for the swift_vapor_app golden-corpus fixture
// (#5569). Mirrors the RouteCollection shape from
// tests/fixtures/dogfood/swift_real_repo/Sources/App/Controllers/UserController.swift
// so the fixture exercises a real controller layer alongside the
// top-level routes.swift entrypoint, instead of a single flat route file.
// Does not reference healthCheck, statusReport, or any other pinned
// discrimination symbol from routes.swift/plain.swift.

import Vapor

struct WidgetController: RouteCollection {
    let store = WidgetStore()

    func boot(routes: RoutesBuilder) throws {
        let widgets = routes.grouped("widgets")
        widgets.get(use: index)
        widgets.get(":widgetId", use: show)
        widgets.post(use: create)
    }

    func index(req: Request) throws -> [Widget] {
        store.all()
    }

    func show(req: Request) throws -> Widget {
        guard let widgetId = req.parameters.get("widgetId"),
              let widget = store.find(id: widgetId) else {
            throw Abort(.notFound)
        }
        return widget
    }

    func create(req: Request) throws -> Widget {
        let widget = try req.content.decode(Widget.self)
        return store.save(widget)
    }
}
