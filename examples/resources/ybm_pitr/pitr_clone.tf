# Clone DB now
resource "ybm_pitr_clone" "example_pitr_clone_now" {
  cluster_id     = "example-cluster-id"
  clone_as       = "example-clone-now-db-clone"
  namespace_name = "example-clone-now-DB"
  namespace_type = "YSQL"
}

# Clone DB to point-in-time
resource "ybm_pitr_clone" "example_pitr_clone_PIT" {
  cluster_id      = "example-cluster-id"
  clone_as        = "example-clone-PIT-db-clone"
  namespace_name  = "example-clone-PIT-DB"
  namespace_type  = "YSQL"
  clone_at_millis = "1234567889"
}