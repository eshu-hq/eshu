import Foundation

// Generic function
func first<T>(_ items: [T]) -> T? {
    return items.first
}

// Generic with constraint
func max<T: Comparable>(_ a: T, _ b: T) -> T {
    return a > b ? a : b
}

// Generic struct
struct Stack<Element> {
    private var items: [Element] = []

    mutating func push(_ item: Element) {
        items.append(item)
    }

    mutating func pop() -> Element? {
        return items.popLast()
    }

    var isEmpty: Bool { items.isEmpty }
    var count: Int { items.count }
}

// Generic class
class Container<T> {
    private var value: T

    init(_ value: T) {
        self.value = value
    }

    func map<U>(_ transform: (T) -> U) -> Container<U> {
        return Container<U>(transform(value))
    }

    func get() -> T { return value }
}

// Where clause
func allEqual<T: Equatable>(_ items: [T]) -> Bool where T: Hashable {
    return Set(items).count <= 1
}

// Extension with generic constraint
extension Array where Element: Numeric {
    func total() -> Element {
        return reduce(0, +)
    }
}
