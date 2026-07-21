// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// Shape informed by public patterns in `scala/scala` and
// `playframework/playframework` at pinned SHA
// 25075e9b9b79954a0f99de515618901818822e62 / bcdc682de2250bbd0f2788bc5acc06f6d66ad5a7
// (recorded as provenance metadata only, never fetched). Synthetic content.

package services

import models.{User, UserNotFoundException}

trait UserRepository {
  def findById(id: String): Option[User]
  def save(user: User): User
}

class InMemoryUserRepository extends UserRepository {
  private var users: Map[String, User] = Map.empty

  def findById(id: String): Option[User] = users.get(id)

  def save(user: User): User = {
    users = users + (user.id -> user)
    user
  }
}

class UserService(repository: UserRepository) {
  def getUser(id: String): User = {
    repository.findById(id).getOrElse(throw new UserNotFoundException(id))
  }

  def createUser(name: String, email: String): User = {
    val user = User(id = generateId(), name = name, email = email)
    repository.save(user)
  }

  private def generateId(): String = java.util.UUID.randomUUID().toString
}
