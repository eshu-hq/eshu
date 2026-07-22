// Widget domain model for the swift_vapor_app golden-corpus fixture (#5569).
// Added alongside routes.swift/plain.swift so the fixture exercises more than
// a two-file Vapor route-handler sliver: a Content-backed model plus an
// in-memory store, in the same shape as
// tests/fixtures/dogfood/swift_real_repo/Sources/App/Models/User.swift.
// Does not reference healthCheck, statusReport, or any other pinned
// discrimination symbol from routes.swift/plain.swift.

import Vapor

struct Widget: Content {
    let id: String
    let name: String
    let quantity: Int
}

final class WidgetStore {
    private var widgets: [String: Widget] = [:]

    func all() -> [Widget] {
        Array(widgets.values)
    }

    func find(id: String) -> Widget? {
        widgets[id]
    }

    func save(_ widget: Widget) -> Widget {
        widgets[widget.id] = widget
        return widget
    }
}
