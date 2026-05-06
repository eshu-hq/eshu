import Foundation

class Animal {
    let name: String
    private(set) var species: String

    init(name: String, species: String) {
        self.name = name
        self.species = species
    }

    func speak() -> String {
        return "\(name) makes a sound"
    }

    deinit {
        print("\(name) is being deallocated")
    }
}

class Dog: Animal {
    init(name: String) {
        super.init(name: name, species: "Canine")
    }

    override func speak() -> String {
        return "\(name) barks"
    }

    func fetch(_ item: String) -> String {
        return "\(name) fetches \(item)"
    }
}

final class GuideDog: Dog {
    let handler: String

    init(name: String, handler: String) {
        self.handler = handler
        super.init(name: name)
    }

    func guide(to destination: String) -> String {
        return "\(name) guides \(handler) to \(destination)"
    }
}
