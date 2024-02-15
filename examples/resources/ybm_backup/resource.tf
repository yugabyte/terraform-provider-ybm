resource "ybm_backup" "example_backup" {
  cluster_id               = "example-cluster-id"
  backup_description       = "example-backup-description"
  retention_period_in_days = 2
}
