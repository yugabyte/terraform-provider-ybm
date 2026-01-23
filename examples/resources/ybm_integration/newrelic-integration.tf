resource "ybm_integration" "newrelic" {
  config_name = "newrelic-example"
  type        = "NEWRELIC"
  newrelic_spec = {
    endpoint    = "<newrelic endpoint url>"
    license_key = "<newrelic license key>"
  }
}
