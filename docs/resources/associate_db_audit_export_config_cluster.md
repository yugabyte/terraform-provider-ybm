---
page_title: "ybm_associate_db_audit_export_config_cluster Resource - YugabyteDB Managed"
description: |-
  The resource to manage DB Audit log configuration for a cluster in YugabyteDB Managed.
---

# ybm_associate_db_audit_export_config_cluster (Resource)

The resource to manage DB Audit log configuration for a cluster in YugabyteDB Managed.


## Example Usage

```terraform
# Cluster associated with a db audit log configuration
resource "ybm_associate_db_audit_export_config_cluster" "sample-db-audit-log-config" {
  cluster_id  = "<Your-Cluster-Id>"
  exporter_id = "<Your-Exported-Id>"
  ysql_config = {
    log_settings = {
      log_catalog        = true
      log_client         = false
      log_relation       = true
      log_level          = "DEBUG1"
      log_statement_once = true
      log_parameter      = false
    }
    statement_classes = ["READ", "WRITE", "ROLE"]
  }
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `cluster_id` (String) ID of the cluster with which this DB Audit log config will be associated
- `exporter_id` (String) ID of the exporter to which the DB Audit logs will be exported
- `ysql_config` (Attributes) The specification for a DB Audit ysql export configuration (see [below for nested schema](#nestedatt--ysql_config))

### Read-Only

- `account_id` (String) ID of the account this DB Audit log configuration belongs to.
- `config_id` (String) ID of the DB Audit log configuration
- `project_id` (String) ID of the project this DB Audit log configuration belongs to.
- `state` (String) The stutus of association of cluster with DB Audit log config

<a id="nestedatt--ysql_config"></a>
### Nested Schema for `ysql_config`

Required:

- `statement_classes` (Set of String) List of ysql statements

Optional:

- `log_settings` (Attributes) Db Audit Ysql Log Settings (see [below for nested schema](#nestedatt--ysql_config--log_settings))

<a id="nestedatt--ysql_config--log_settings"></a>
### Nested Schema for `ysql_config.log_settings`

Optional:

- `log_catalog` (Boolean) These system catalog tables record system (as opposed to user) activity, such as metadata lookups and from third-party tools performing lookups
- `log_client` (Boolean) Enable this option to echo log messages directly to clients such as ysqlsh and psql
- `log_level` (String) Sets the severity level of logs written to clients
- `log_parameter` (Boolean) Include the parameters that were passed with the statement in the logs
- `log_relation` (Boolean) Create separate log entries for each relation (TABLE, VIEW, and so on) referenced in a SELECT or DML statement
- `log_statement_once` (Boolean) Enable this setting to only include statement text and parameters for the first entry for a statement or sub-statement combination