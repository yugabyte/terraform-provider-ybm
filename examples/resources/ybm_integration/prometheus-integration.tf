resource "ybm_integration" "prometheus" {
  config_name = "prometheus-example"
  type        = "PROMETHEUS"
  prometheus_spec = {
    endpoint = "http://my-prometheus-endpoint/api/v1/otlp"
  }
}
