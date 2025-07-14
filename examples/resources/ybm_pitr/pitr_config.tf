resource "ybm_pitr_config" "example_pitr" {
  cluster_id               = "example-cluster-id"
  namespace_name           = "example-PITR-DB"
  namespace_type           = "YSQL"
  retention_period_in_days = 7
}