# Example for Datadog
resource "ybm_metrics_exporter" "datadog" {
  config_name = "datadogTest"
  type        = "DATADOG"
  datadog_spec = {
    api_key = "Your api key"
    site    = "datadoghq.com"
  }
}


# Example for Grafana
resource "ybm_metrics_exporter" "gwenngrafna" {
  config_name = "grafanaTest"
  type        = "GRAFANA"
  grafana_spec = {
    access_policy_token = "your access policy token"
    instance_id         = "111111"
    org_slug            = "orgtest"
    zone                = "prod-us-east-0"
  }
}
