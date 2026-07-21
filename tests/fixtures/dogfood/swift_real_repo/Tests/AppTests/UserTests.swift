// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// XCTest-shaped test file mirroring the Tests/<Target>Tests layout
// convention used by Vapor-style server apps. Synthetic content.

import XCTest
@testable import App

final class UserTests: XCTestCase {
    func testStoreSaveAndFind() throws {
        let store = UserStore()
        let saved = store.save(User(id: "u-1", name: "Dogfood", email: "dogfood@example.invalid"))
        XCTAssertEqual(saved.id, "u-1")
        XCTAssertNotNil(store.find(id: "u-1"))
    }

    func testStoreAllReturnsAllUsers() throws {
        let store = UserStore()
        _ = store.save(User(id: "u-1", name: "One", email: "one@example.invalid"))
        _ = store.save(User(id: "u-2", name: "Two", email: "two@example.invalid"))
        XCTAssertEqual(store.all().count, 2)
    }
}
