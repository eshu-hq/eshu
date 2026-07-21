// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// Play controller-action shape informed by public patterns in
// `playframework/playframework` at pinned SHA
// bcdc682de2250bbd0f2788bc5acc06f6d66ad5a7 (provenance metadata only, never
// fetched). Synthetic content, hand-authored for this fixture.

package controllers

import models.User
import services.UserService

class UserController(userService: UserService) {

  def show(id: String): User = {
    userService.getUser(id)
  }

  def create(name: String, email: String): User = {
    userService.createUser(name, email)
  }

  def healthCheck(): String = {
    "ok"
  }
}

object UserControllerApp extends App {
  val repository = new services.InMemoryUserRepository()
  val service = new UserService(repository)
  val controller = new UserController(service)
  controller.create("dogfood", "dogfood@example.invalid")
  println(controller.healthCheck())
}
