# journal-reminder infrastructure, managed by OpenTofu.
#
# Adopts the previously hand-created (clickops) resources via `tofu import`,
# migrates the function off the deprecated go1.x runtime to provided.al2023,
# and puts the schedule + Google Doc IDs under version control.
#
# The Gmail app password is NOT managed here: it lives in an SSM SecureString
# (see var.sender_password_ssm_param) read by the function at runtime, so the
# secret never enters this config or the state file.

terraform {
  required_version = ">= 1.10.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.60"
    }
  }
  backend "s3" {
    key          = "journal-reminder/journal-reminder.tfstate"
    use_lockfile = true # S3-native locking, no DynamoDB
  }
}

variable "aws_region" {
  type    = string
  default = "us-east-2"
}

variable "schedule_expression" {
  type    = string
  default = "cron(0 8 * * ? *)" # daily at 08:00 UTC
}

variable "sender_email" {
  type    = string
  default = "davidbot3388@gmail.com"
}

variable "recipient_email" {
  type    = string
  default = "davidhand1988@gmail.com"
}

variable "sender_password_ssm_param" {
  type    = string
  default = "/journal-reminder/sender-password"
}

variable "document_ids" {
  type        = list(string)
  description = "Google Doc IDs of the journals to sample from."
  default = [
    "1ihbEV7nWlICoZlQr21EbJHRvdz0Ab7U0W5goy7s7CBQ",
    "1LJkz4sjncy5ri8_afMj-naMkCJQo7E9WWFlpNWbUeHg",
    "16oNVwBr8qDjIatChD0MKtxM8p_yMTxWHdFD9GOey9LE",
    "1ofWTGlasjBpg7LtoLnN8HxSAtUeMUcGi5IGVT6ir5PM",
    "1G5pGA-QH1-3RExcYlVnMK8GUoHaiNk8-05k7qsFhH-k",
    "1w36Ch1NO0USBATb_eoMCzTuYFAdZ9cp9CSRPDVs7SVU",
    "1LdRZX4Dh2CtAzo8Ik8K4FXSnwLeeBXvZdee28wy6AY4",
    "1YYIeXkPs3e6amIhrivl7qXCahV9c2GtHsi45-StUXFA",
  ]
}

provider "aws" {
  region = var.aws_region
  default_tags {
    tags = { Project = "journal-reminder" }
  }
}

data "aws_caller_identity" "current" {}

locals {
  function_name = "journal-reminder"
  ssm_param_arn = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${var.sender_password_ssm_param}"
}

# --- Lambda execution role (fresh, least-privilege) ---
resource "aws_iam_role" "lambda" {
  name = "journal-reminder-exec"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "logs" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# Read (and KMS-decrypt) only the one sender-password parameter.
resource "aws_iam_role_policy" "ssm" {
  name = "read-sender-password"
  role = aws_iam_role.lambda.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["ssm:GetParameter"]
        Resource = local.ssm_param_arn
      },
      {
        Effect   = "Allow"
        Action   = ["kms:Decrypt"]
        Resource = "*"
        Condition = {
          StringEquals = { "kms:ViaService" = "ssm.${var.aws_region}.amazonaws.com" }
        }
      },
    ]
  })
}

# --- The function itself (zip package, provided.al2023) ---
resource "aws_lambda_function" "journal_reminder" {
  function_name    = local.function_name
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["x86_64"]
  filename         = "${path.module}/../dist.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist.zip")
  timeout          = 15
  memory_size      = 512

  environment {
    variables = {
      SENDER_EMAIL              = var.sender_email
      RECIPIENT_EMAIL           = var.recipient_email
      SENDER_PASSWORD_SSM_PARAM = var.sender_password_ssm_param
    }
  }
}

resource "aws_cloudwatch_log_group" "lambda" {
  name              = "/aws/lambda/${local.function_name}"
  retention_in_days = 30
}

# --- EventBridge (Events) rule + target carrying the doc IDs ---
resource "aws_cloudwatch_event_rule" "schedule" {
  name                = "journal-reminder-every-three-days"
  description         = "Run daily at 8 AM UTC"
  schedule_expression = var.schedule_expression
}

resource "aws_cloudwatch_event_target" "lambda" {
  rule  = aws_cloudwatch_event_rule.schedule.name
  arn   = aws_lambda_function.journal_reminder.arn
  input = jsonencode({ documentIds = var.document_ids })
}

resource "aws_lambda_permission" "eventbridge" {
  statement_id  = "AllowEventBridgeInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.journal_reminder.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.schedule.arn
}
