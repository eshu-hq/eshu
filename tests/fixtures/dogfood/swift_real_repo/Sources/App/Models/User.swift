// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// Sources/App/Models layout shape informed by public patterns in
// Vapor-style server apps (vapor/vapor, vapor/template). Synthetic,
// hand-authored for this fixture; no external repo content is vendored
// here.

import Foundation

struct User: Codable {
    let id: String
    let name: String
    let email: String
}

struct UserNotFoundError: Error {
    let userId: String
}

final class UserStore {
    private var users: [String: User] = [:]

    func find(id: String) -> User? {
        return users[id]
    }

    func save(_ user: User) -> User {
        users[user.id] = user
        return user
    }

    func all() -> [User] {
        return Array(users.values)
    }
}
