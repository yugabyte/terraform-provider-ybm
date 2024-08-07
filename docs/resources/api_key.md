---
page_title: "ybm_api_key Resource - YugabyteDB Aeon"
description: |-
  The resource to issue an API Key in YugabyteDB Aeon.
---

# ybm_api_key (Resource)

The resource to issue an API Key in YugabyteDB Aeon.


## Example Usage

To issue an API Key with predefined built-in roles like Admin, Developer, Viewer

```terraform
resource "ybm_api_key" "example_api_key" {
  name        = "example-api-key-name"
  description = "example API Key description" #Optional
  duration    = 10
  unit        = "Hours"
  role_name   = "Developer"
}
```

To issue an API Key with custom user defined roles

```terraform
resource "ybm_api_key" "example_custom_role_api_key" {
  name        = "example-api-key-name"
  description = "example API Key description" #Optional
  duration    = 1
  unit        = "Months"
  role_name   = "example-custom-role-name"
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `duration` (Number) The duration for which the API Key will be valid. 0 denotes that the key will never expire.
- `name` (String) The name of the API Key.
- `role_name` (String) The role of the API Key.
- `unit` (String) The time units for which the API Key will be valid. Available options are Hours, Days, and Months.

### Optional

- `api_key_id` (String) The ID of the API Key. Created automatically when an API Key is created. Use this ID to get a specific API Key.
- `description` (String) The description of the API Key.

### Read-Only

- `account_id` (String) The ID of the account this user belongs to.
- `api_key` (String, Sensitive) The API Key.
- `date_created` (String) The creation time of the API Key.
- `expiration` (String) The expiry time of the API Key.
- `issuer` (String) The issuer of the API Key.
- `last_used` (String) The last used time of the API Key.
- `project_id` (String) The ID of the project this user belongs to.
- `status` (String) The status of the API Key.