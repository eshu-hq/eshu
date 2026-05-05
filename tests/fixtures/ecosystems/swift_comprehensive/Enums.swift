import Foundation

enum Direction: String, CaseIterable {
    case north, south, east, west

    var opposite: Direction {
        switch self {
        case .north: return .south
        case .south: return .north
        case .east: return .west
        case .west: return .east
        }
    }
}

enum Result<Success, Failure: Error> {
    case success(Success)
    case failure(Failure)

    func map<T>(_ transform: (Success) -> T) -> Result<T, Failure> {
        switch self {
        case .success(let value):
            return .success(transform(value))
        case .failure(let error):
            return .failure(error)
        }
    }
}

enum NetworkError: Error, CustomStringConvertible {
    case notFound
    case unauthorized
    case serverError(code: Int, message: String)

    var description: String {
        switch self {
        case .notFound: return "Not Found"
        case .unauthorized: return "Unauthorized"
        case .serverError(let code, let message):
            return "Server Error \(code): \(message)"
        }
    }
}

// Enum with raw values and computed properties
enum Planet: Int {
    case mercury = 1, venus, earth, mars, jupiter, saturn, uranus, neptune

    var isInner: Bool {
        return rawValue <= 4
    }
}
