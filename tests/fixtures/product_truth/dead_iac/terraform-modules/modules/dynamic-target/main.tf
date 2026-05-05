variable "service_name" {
  type = string
}

resource "aws_lambda_function" "this" {
  function_name = var.service_name
  role          = "arn:aws:iam::123456789012:role/fixture"
  handler       = "bootstrap"
  runtime       = "provided.al2"
  filename      = "bootstrap.zip"
}
