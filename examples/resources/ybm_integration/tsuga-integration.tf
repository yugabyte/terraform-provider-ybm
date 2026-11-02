resource "ybm_integration" "tsuga" {
  config_name = "tsuga-example"
  type        = "TSUGA"
  tsuga_spec = {
    endpoint  = "<tsuga otlp endpoint url>" # e.g. intake.yourcompany.tsuga.com/api/v1/otlp
    api_token = "<tsuga api token>"
  }
}
