terraform {
  required_providers {
    vault = {
      source = "hashicorp/vault"
    }
    cassandra = {
      source = "kyawmyintthein/cassandra"
    }
  }
}

variable "vault_mount" {
  type        = string
  description = "Vault KV v2 mount name."
}

variable "vault_secret_name" {
  type        = string
  description = "Vault secret path containing Cassandra credentials."
}

data "vault_kv_secret_v2" "cassandra" {
  mount = var.vault_mount
  name  = var.vault_secret_name
}

provider "cassandra" {
  hosts            = ["127.0.0.1"]
  port             = 9042
  local_datacenter = "dc1"
  username         = data.vault_kv_secret_v2.cassandra.data["username"]
  password         = data.vault_kv_secret_v2.cassandra.data["password"]
}

resource "cassandra_system_level_profile" "default_twcs" {
  name = "default_twcs"

  compaction = {
    class = "org.apache.cassandra.db.compaction.TimeWindowCompactionStrategy"
    options = {
      compaction_window_size = "1"
      compaction_window_unit = "DAYS"
    }
  }

  gc_grace_seconds = 86400
}
