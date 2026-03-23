terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
    cassandra = {
      source = "kyawmyintthein/cassandra"
    }
  }
}

variable "cassandra_secret_id" {
  type        = string
  description = "AWS Secrets Manager secret containing username/password JSON."
}

data "aws_secretsmanager_secret_version" "cassandra" {
  secret_id = var.cassandra_secret_id
}

locals {
  cassandra_auth = jsondecode(data.aws_secretsmanager_secret_version.cassandra.secret_string)
}

provider "cassandra" {
  hosts            = ["127.0.0.1"]
  port             = 9042
  local_datacenter = "dc1"
  username         = local.cassandra_auth.username
  password         = local.cassandra_auth.password
}

resource "cassandra_user_level_table" "events" {
  keyspace      = "app"
  table_name    = "events"
  if_not_exists = true

  columns = [
    {
      name = "tenant_id"
      type = "text"
    },
    {
      name = "event_id"
      type = "timeuuid"
    }
  ]

  partition_keys = ["tenant_id"]

  clustering_keys = [
    {
      name  = "event_id"
      order = "DESC"
    }
  ]
}
