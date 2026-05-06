variable "database_url" {
  type = string
  default = "postgres://postgres:postgres@localhost:5436/event_ticketing?sslmode=disable"
}

variable "staging_db_url" {
  type = string
  default = "postgres://user:pass@staging-host:5432/db?sslmode=require"
}

variable "production_db_url" {
  type = string
  default = "postgres://user:pass@prod-host:5432/db?sslmode=require"
}

env "local" {
  src = "file://schema.hcl"
  dev = "postgres://postgres:postgres@localhost:5436/event_ticketing_dev?sslmode=disable"
  url = var.database_url
  migration {
    dir = "file://migrations"
  }
}

env "staging" {
  src = "file://schema.hcl"
  url = var.staging_db_url
  migration {
    dir = "file://migrations"
  }
}

env "production" {
  src = "file://schema.hcl"
  url = var.production_db_url
  migration {
    dir = "file://migrations"
  }
}