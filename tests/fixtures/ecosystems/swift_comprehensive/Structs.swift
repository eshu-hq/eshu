import Foundation

struct Point {
    var x: Double
    var y: Double

    func distance(to other: Point) -> Double {
        let dx = x - other.x
        let dy = y - other.y
        return (dx * dx + dy * dy).squareRoot()
    }

    mutating func translate(dx: Double, dy: Double) {
        x += dx
        y += dy
    }

    static let origin = Point(x: 0, y: 0)
}

struct Config {
    let host: String
    let port: Int
    let debug: Bool

    init(host: String = "localhost", port: Int = 8080, debug: Bool = false) {
        self.host = host
        self.port = port
        self.debug = debug
    }

    var url: String {
        return "http://\(host):\(port)"
    }
}

struct Size: Equatable, CustomStringConvertible {
    let width: Double
    let height: Double

    var area: Double { width * height }

    var description: String {
        return "\(width)x\(height)"
    }
}
