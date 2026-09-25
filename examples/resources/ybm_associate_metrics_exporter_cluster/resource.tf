resource "ybm_associate_metrics_exporter_cluster" "metrics-srcluster" {
  cluster_id = ybm_cluster.single_region_cluster.cluster_id
  # cluster_name = ybm_cluster.single_region_cluster.cluster_name # Use instead of cluster_id
  config_id = ybm_integration.test.config_id
  # config_name = ybm_integration.test.config_name # Use instead of config_id
  export_state = "Active"
  depends_on   = [ybm_cluster.single_region_cluster, ybm_integration.test]
}
