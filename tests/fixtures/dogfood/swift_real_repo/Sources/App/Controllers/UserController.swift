// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// Vapor route-registration shape informed by public patterns in Vapor-style
// server apps. Synthetic content, hand-authored for this fixture.

import Vapor

struct UserController: RouteCollection {
    let store = UserStore()

    func boot(routes: RoutesBuilder) throws {
        let users = routes.grouped("users")
        users.get(use: index)
        users.get(":userId", use: show)
        users.post(use: create)
    }

    func index(req: Request) throws -> [User] {
        return store.all()
    }

    func show(req: Request) throws -> User {
        guard let userId = req.parameters.get("userId") else {
            throw UserNotFoundError(userId: "")
        }
        guard let user = store.find(id: userId) else {
            throw UserNotFoundError(userId: userId)
        }
        return user
    }

    func create(req: Request) throws -> User {
        let user = try req.content.decode(User.self)
        return store.save(user)
    }
}
