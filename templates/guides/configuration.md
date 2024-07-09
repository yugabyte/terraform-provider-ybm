---
page_title: "Configuration"
description: Configuring the YugabyteDB Aeon Provider
---

# Configuring the YugabyteDB Aeon Provider

The YugabyteDB Aeon Provider can be used to interact with the resources provided by YugabyteDB Aeon. To interact with YugabyteDB Aeon, the provider needs to be authenticated. For authentication, the environment variable `TF_VAR_auth_token` needs to be set to the token obtained from YugabyteDB Aeon. The steps to obtain and set the authentication token are given in the following section.

## Obtaining the authentication token

- Login to [YugabyteDB Aeon](https://cloud.yugabyte.com/)
- Select `Admin` from the navigation bar on the left-hand side
- In the `Admin` page, select `API Keys` from the tabs on the top
- Create an API Key using the `Create API Key` button at the top on the righ-hand side
- Store the generate API Key safely and use it as the authentication token
- `export TF_VAR_auth_token=<api-key>`