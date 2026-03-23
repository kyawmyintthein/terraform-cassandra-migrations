terraform {
  required_providers {
    cassandra = {
      source = "kyawmyintthein/cassandra"
    }
  }
}

provider "cassandra" {
  hosts            = ["127.0.0.1"]
  port             = 9042
  local_datacenter = "dc1"
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
  comment          = "Default profile managed by DB admin Terraform"

  additional_options = {
    caching = "{'keys':'ALL','rows_per_partition':'NONE'}"
  }
}
