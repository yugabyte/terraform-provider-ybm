# Example for Datadog
resource "ybm_metrics_exporter" "datadog" {
  config_name = "datadogTest"
  type = "DATADOG"
  datadog_spec = {
    api_key = "Your api key"
    site ="datadoghq.com"
  }
}


# Example for Grafana
resource "ybm_metrics_exporter" "gwenngrafna" {
  config_name = "grafanaTest"
  type = "GRAFANA"
  grafana_spec = {
    api_key = "your api key"
    instance_id ="111111"
    org_slug= "orgtest"
    endpoint = "https://otlp-gateway-prod-us-east-0.grafana.net/otlp"
  }
}