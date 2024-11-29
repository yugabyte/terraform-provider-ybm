resource "ybm_associate_metrics_exporter_cluster" "metrics-srcluster" {
  cluster_id = ybm_cluster.single_region_cluster.cluster_id
  config_id  = ybm_integration.test.config_id
  depends_on = [ybm_cluster.single_region_cluster, ybm_integration.test]
}
