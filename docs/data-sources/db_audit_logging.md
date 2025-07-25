---
# generated by https://github.com/hashicorp/terraform-plugin-docs
page_title: "ybm_db_audit_logging Data Source - YugabyteDB Aeon"
subcategory: ""
description: |-
  The data source to fetch DB Audit log configuration for a cluster given cluster ID in YugabyteDB Aeon.
---

# ybm_db_audit_logging (Data Source)

The data source to fetch DB Audit log configuration for a cluster given cluster ID in YugabyteDB Aeon.

## Example Usage

```terraform
data "ybm_db_audit_logging" "example_db_audit_log_config" {
  cluster_id = "<cluster-id>"
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `cluster_id` (String) ID of the cluster from which DB Audit Logs will be exported

### Read-Only

- `account_id` (String) ID of the account this DB Audit log configuration belongs to.
- `cluster_name` (String) Name of the cluster from which DB Audit Logs will be exported
- `config_id` (String) ID of the DB Audit log configuration
- `integration_id` (String) ID of the integration to which the DB Audit Logs will be exported
- `integration_name` (String) Name of the integration to which the DB Audit Logs will be exported
- `project_id` (String) ID of the project this DB Audit log configuration belongs to.
- `state` (String) The status of DB Audit Logging on the cluster
- `ysql_config` (Attributes) The specification for a DB Audit ysql export configuration (see [below for nested schema](#nestedatt--ysql_config))

<a id="nestedatt--ysql_config"></a>
### Nested Schema for `ysql_config`

Read-Only:

- `log_settings` (Attributes) Db Audit Ysql Log Settings (see [below for nested schema](#nestedatt--ysql_config--log_settings))
- `statement_classes` (Set of String) List of ysql statements

<a id="nestedatt--ysql_config--log_settings"></a>
### Nested Schema for `ysql_config.log_settings`

Read-Only:

- `log_catalog` (Boolean) These system catalog tables record system (as opposed to user) activity, such as metadata lookups and from third-party tools performing lookups
- `log_client` (Boolean) Enable this option to echo log messages directly to clients such as ysqlsh and psql
- `log_level` (String) Sets the severity level of logs written to clients
- `log_parameter` (Boolean) Include the parameters that were passed with the statement in the logs
- `log_relation` (Boolean) Create separate log entries for each relation (TABLE, VIEW, and so on) referenced in a SELECT or DML statement
- `log_statement_once` (Boolean) Enable this setting to only include statement text and parameters for the first entry for a statement or sub-statement combination
