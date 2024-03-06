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
