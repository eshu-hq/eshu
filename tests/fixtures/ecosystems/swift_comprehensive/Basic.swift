import Foundation

// Functions
func greet(_ name: String) -> String {
    return "Hello, \(name)!"
}

func add(_ a: Int, _ b: Int) -> Int {
    return a + b
}

func divide(_ a: Double, by b: Double) throws -> Double {
    guard b != 0 else {
        throw CalculationError.divisionByZero
    }
    return a / b
}

// Variadic
func sum(_ numbers: Int...) -> Int {
    return numbers.reduce(0, +)
}

// Closure parameter
func transform(_ items: [String], using block: (String) -> String) -> [String] {
    return items.map(block)
}

// Enum
enum CalculationError: Error {
    case divisionByZero
    case overflow
    case invalidInput(String)
}

// Typealias
typealias CompletionHandler = (Result<String, Error>) -> Void
