resource "ybm_read_replicas" "example_read_replica" {
  read_replicas_info = [ 
    {
      cloud_type = "GCP"
      num_replicas = 1
      num_nodes = 1
      region = "us-east4"
      vpc_id = "example-vpc-id"
      node_config = {
        num_cores = 2
        memory_mb = 8192
        disk_size_gb = 10
      }
    }
  ]
  primary_cluster_id = "example-primary-cluster-id"
}
