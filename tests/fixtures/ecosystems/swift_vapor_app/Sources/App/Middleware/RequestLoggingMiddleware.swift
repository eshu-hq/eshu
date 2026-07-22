// Request-logging middleware for the swift_vapor_app golden-corpus fixture
// (#5569). Exercises Vapor's Middleware protocol conformance shape alongside
// the route-handler and model/controller layers already in this fixture, so
// the fixture reads as an app-shaped Vapor server rather than a single route
// file. Does not reference healthCheck, statusReport, or any other pinned
// discrimination symbol from routes.swift/plain.swift.

import Vapor

final class RequestLoggingMiddleware: Middleware {
    func respond(to request: Request, chainingTo next: Responder) -> EventLoopFuture<Response> {
        request.logger.info("\(request.method) \(request.url.path)")
        return next.respond(to: request)
    }
}
