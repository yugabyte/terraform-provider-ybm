# DB audit log configuration for a cluster
resource "ybm_db_audit_logging" "sample-db-audit-log-config" {
  cluster_id       = "<Your-Cluster-Id>"
  integration_name = "<Your-Integration-Name>"
  ysql_config = {
    log_settings = {
      log_catalog        = true
      log_client         = false
      log_relation       = true
      log_level          = "LOG"
      log_statement_once = true
      log_parameter      = false
    }
    statement_classes = ["READ", "WRITE", "ROLE"]
  }
}
