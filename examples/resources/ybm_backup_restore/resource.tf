resource "ybm_backup_restore" "example" {
  backup_id         = "example-backup-id"
  target_cluster_id = ybm_cluster.example.cluster_id
  use_roles         = false

  ysql_databases = ["yugabyte"]
  ycql_keyspaces = ["example_keyspace"]

  ysql_databases_rename = [
    {
      backup_database  = "yugabyte"
      restore_database = "yugabyte_restored"
    },
  ]

  ycql_keyspaces_rename = [
    {
      backup_database  = "example_keyspace"
      restore_database = "example_keyspace_restored"
    },
  ]
}
