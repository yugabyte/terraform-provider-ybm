resource "ybm_role" "example_role" {
  role_name        = "example_role_name"
  role_description = "example_role_description" #Optional
  permission_list = [
    {
      resource_type    = "CLUSTER"
      operation_groups = ["READ"]
    },
    {
      resource_type    = "READ_REPLICA"
      operation_groups = ["READ", "CREATE"]
    }
  ]
}