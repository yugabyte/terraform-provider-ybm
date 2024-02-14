resource "ybm_api_key" "example_custom_role_api_key" {
  name        = "example-api-key-name"
  description = "example API Key description" #Optional
  duration    = 1
  unit        = "Months"
  role_name   = "example-custom-role-name"
}