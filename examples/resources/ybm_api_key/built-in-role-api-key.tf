resource "ybm_api_key" "example_api_key" {
    name = "example-api-key-name"
    description = "example API Key description" #Optional
    duration = 10
    unit = "Hours"
    role_name = "Developer"
}