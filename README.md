# Cassandra Schema Migration Terraform Provider

This provider manages Cassandra schema changes in two layers:

- `cassandra_user_level_table`: table shape, keys, additive/removal column migrations, and SAI indexes.
- `cassandra_system_level_table_settings`: operational table settings such as compaction strategy and table options.

## Project status

This repository is set up as an open source project with contribution, security, and review guidance for collaborators.

See:

- [CONTRIBUTING.md](CONTRIBUTING.md)
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)
- [SECURITY.md](SECURITY.md)

## Recommended operating model

Recommended default: split ownership into two Terraform projects and two Terraform states.

- DB admin or platform team owns system-level profiles and exception settings.
- Client app team owns user-level table definition.

This split reduces risk because application teams can evolve table shape without accidentally changing compaction or storage behavior, while DB admins keep control over operational tuning. It also solves the "half-created table" problem by letting the app team require an admin-managed system profile during table creation.

Use the central PR-review model only when:

- one team already reviews every schema change
- the organization is small enough that review bottlenecks are acceptable
- you want one place for all schema governance more than you want team autonomy

## Repo examples

- Split ownership example for app teams: `examples/split-ownership/user-level`
- Split ownership example for DB admins: `examples/split-ownership/system-level`
- Central review example: `examples/central-review`
- AWS Secrets Manager auth example: `examples/external-secrets/aws-secrets-manager`
- Vault auth example: `examples/external-secrets/vault`

## Apply order

When you use separate Terraform states:

1. Apply the system-level profile Terraform first so approved profiles exist in Cassandra.
2. Apply the user-level Terraform after that so the table is created with the required profile already attached.
3. Optionally apply table-specific `cassandra_system_level_table_settings` afterward for one-off overrides.

The provider keeps responsibilities separate, but app teams can now require a DB-admin profile up front so production tables are not created without essential compaction and operational defaults.

## Provider

```hcl
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
```

The provider also supports environment-variable configuration, which is useful when CI/CD, Vault Agent, Kubernetes secrets, or another secret delivery system injects credentials at runtime:

- `CASSANDRA_HOSTS` as a comma-separated host list
- `CASSANDRA_PORT`
- `CASSANDRA_LOCAL_DATACENTER`
- `CASSANDRA_USERNAME`
- `CASSANDRA_PASSWORD`
- `CASSANDRA_CONSISTENCY`
- `CASSANDRA_TIMEOUT_SECONDS`

Example:

```bash
export CASSANDRA_HOSTS=10.0.0.10,10.0.0.11
export CASSANDRA_LOCAL_DATACENTER=ap-southeast-2
export CASSANDRA_USERNAME=$(vault kv get -field=username kv/cassandra/prod)
export CASSANDRA_PASSWORD=$(vault kv get -field=password kv/cassandra/prod)
terraform apply
```

You can still set `username` and `password` directly in the provider block when those values come from another Terraform provider or data source.

## External secret sources

The Cassandra provider does not need a built-in AWS or Vault integration. Terraform can resolve credentials from other providers and pass the resulting sensitive values into the Cassandra provider.

### AWS Secrets Manager

```hcl
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

data "aws_secretsmanager_secret_version" "cassandra" {
  secret_id = var.cassandra_secret_id
}

locals {
  cassandra_auth = jsondecode(data.aws_secretsmanager_secret_version.cassandra.secret_string)
}

provider "cassandra" {
  hosts            = ["127.0.0.1"]
  local_datacenter = "dc1"
  username         = local.cassandra_auth.username
  password         = local.cassandra_auth.password
}
```

Expected secret JSON:

```json
{
  "username": "schema_migrator",
  "password": "super-secret"
}
```

### Vault KV v2

```hcl
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

data "vault_kv_secret_v2" "cassandra" {
  mount = var.vault_mount
  name  = var.vault_secret_name
}

provider "cassandra" {
  hosts            = ["127.0.0.1"]
  local_datacenter = "dc1"
  username         = data.vault_kv_secret_v2.cassandra.data["username"]
  password         = data.vault_kv_secret_v2.cassandra.data["password"]
}
```

This pattern also works with any other secret source that can expose credentials as Terraform expressions or environment variables.

## Admin-managed profile

```hcl
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
  comment          = "Default production profile"
}
```

## User-level schema

```hcl
resource "cassandra_user_level_table" "events" {
  keyspace                = "app"
  table_name              = "events"
  if_not_exists           = true
  required_system_profile = "default_twcs"

  columns = [
    {
      name = "tenant_id"
      type = "text"
    },
    {
      name = "event_id"
      type = "timeuuid"
    },
    {
      name = "event_type"
      type = "text"
    },
    {
      name = "payload"
      type = "text"
    }
  ]

  partition_keys = ["tenant_id"]

  clustering_keys = [
    {
      name  = "event_id"
      order = "DESC"
    }
  ]

  sai_indexes = [
    {
      name   = "events_event_type_sai"
      column = "event_type"
    }
  ]
}
```

## System-level settings overrides

```hcl
resource "cassandra_system_level_table_settings" "events" {
  keyspace   = "app"
  table_name = "events"

  compaction = {
    class = "org.apache.cassandra.db.compaction.TimeWindowCompactionStrategy"
    options = {
      compaction_window_size = "1"
      compaction_window_unit = "DAYS"
    }
  }

  gc_grace_seconds = 86400
  comment          = "Managed by Terraform"

  additional_options = {
    caching = "{'keys':'ALL','rows_per_partition':'NONE'}"
  }
}
```

## Ownership guidance

Recommended ownership split:

- `cassandra_user_level_table`: app or service team
- `cassandra_system_level_profile`: DB admin or platform team
- `cassandra_system_level_table_settings`: DB admin or platform team

Avoid having two Terraform states manage the same concern. A clean boundary is:

- user-level owns columns, primary key layout, clustering order, SAI definitions, and the choice of approved profile
- system-level profile owns default compaction and operational policy
- system-level table settings owns table-specific exceptions only

## Behavior notes

- Partition keys and clustering keys are treated as immutable.
- Column type changes are rejected because Cassandra does not safely support all in-place type migrations.
- Resource deletion removes Terraform state only; it does not drop live tables or indexes.

## Development

Local checks:

```bash
gofmt -w .
go vet ./...
go test ./...
```
