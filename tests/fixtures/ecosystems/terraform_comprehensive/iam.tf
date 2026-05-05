resource "aws_iam_role" "irsa_role" {
  name               = "${local.name_prefix}-irsa"
  assume_role_policy = data.aws_iam_policy_document.trust.json

  tags = local.common_tags
}

resource "aws_iam_role_policy_attachment" "s3_access" {
  role       = aws_iam_role.irsa_role.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess"
}
