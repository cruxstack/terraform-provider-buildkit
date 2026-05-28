resource "buildkit_artifact" "lambda" {
  build_context     = "${path.module}/app"
  dockerfile        = "Dockerfile"
  target            = "package"
  artifact_src_path = "/tmp/package.zip"
  artifact_src_type = "zip"
  artifact_dst_path = "${path.module}/dist/package.zip"

  build_args = {
    NODE_ENV = "production"
  }
}

# consume the produced artifact, e.g. an AWS Lambda deployment package.
resource "aws_lambda_function" "this" {
  function_name    = "example"
  runtime          = "nodejs20.x"
  handler          = "index.handler"
  filename         = buildkit_artifact.lambda.artifact_path
  source_code_hash = buildkit_artifact.lambda.artifact_sha256
  role             = aws_iam_role.this.arn
}
