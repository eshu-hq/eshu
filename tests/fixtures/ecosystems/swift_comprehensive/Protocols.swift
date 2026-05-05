import Foundation

protocol Identifiable {
    var id: String { get }
}

protocol Describable {
    func describe() -> String
}

protocol Repository {
    associatedtype Entity: Identifiable

    func findById(_ id: String) -> Entity?
    func findAll() -> [Entity]
    func save(_ entity: Entity)
    func delete(_ id: String) -> Bool
}

// Protocol with default implementation
protocol Logger {
    func log(_ level: String, _ message: String)
}

extension Logger {
    func info(_ message: String) { log("INFO", message) }
    func warn(_ message: String) { log("WARN", message) }
    func error(_ message: String) { log("ERROR", message) }
}

// Protocol composition
typealias Trackable = Identifiable & Describable

struct User: Trackable {
    let id: String
    let name: String
    let email: String

    func describe() -> String {
        return "User(\(name), \(email))"
    }
}

class InMemoryStore<T: Identifiable>: Repository, Logger {
    typealias Entity = T

    private var store: [String: T] = [:]

    func findById(_ id: String) -> T? {
        info("Finding: \(id)")
        return store[id]
    }

    func findAll() -> [T] { Array(store.values) }

    func save(_ entity: T) {
        store[entity.id] = entity
        info("Saved: \(entity.id)")
    }

    func delete(_ id: String) -> Bool {
        return store.removeValue(forKey: id) != nil
    }

    func log(_ level: String, _ message: String) {
        print("[\(level)] \(message)")
    }
}
