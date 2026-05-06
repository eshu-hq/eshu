resource "aws_db_instance" "app" {
  identifier = "eshu-fixture-db"
  engine     = "postgres"
  username   = "postgres"
  password   = "postgres"
}
