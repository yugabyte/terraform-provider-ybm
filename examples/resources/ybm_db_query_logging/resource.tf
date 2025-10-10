# DB query log configuration for a cluster
resource "ybm_db_query_logging" "sample-db-query-log-config" {
  cluster_id       = "<Your-Cluster-Id>"
  integration_name = "<Your-Integration-Name>"
  log_config = {
    log_settings = {
      debug_print_plan           = false
      log_connections            = true
      log_disconnections         = true
      log_duration               = true
      log_error_verbosity        = "DEFAULT"
      log_line_prefix            = "%m : %r : %u @ %d : [ %p ] : ( %e ) : %a "
      log_min_duration_statement = 100
      log_min_error_statement    = "ERROR"
      log_statement              = "ALL"
    }
  }
}
