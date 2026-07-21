// Dogfood real-repo fixture for eshu-hq/eshu#5399.
//
// Shape informed by public patterns in `playframework/playframework`
// (app/models layering) at pinned SHA bcdc682de2250bbd0f2788bc5acc06f6d66ad5a7,
// recorded here as provenance metadata only -- this file is synthetic,
// hand-authored for this fixture, never fetched or vendored from that repo.

package models

case class User(id: String, name: String, email: String)

object User {
  def empty: User = User(id = "", name = "", email = "")

  def validate(user: User): Boolean = {
    user.id.nonEmpty && user.email.contains("@")
  }
}

class UserNotFoundException(userId: String) extends RuntimeException(s"user not found: $userId")
