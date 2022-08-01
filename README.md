# YugabyteDB Managed Terraform Provider

The Yugabyte Cloud provider is used to create, read, update, and delete clusters on the cloud. Any cloud site will work as long as the API is compatible (cloudportal, devcloud, localhost, etc.)

## Build Provier
Run the following command to build the provider

```shell
make install
```

## Run Example

First, build and install the provider.

```shell
make install
```

Then, run the following command to initialize the workspace and apply the sample configuration.

**Note: to run this without errors you will need to do the following**
- change the email and password in `examples/main.tf` according to your local configuration
- have an instance of the apiserver running locally

```shell
cd examples && ./run_example.sh
```


## Generate Documentation
Install tfplugindocs from [here](https://github.com/hashicorp/terraform-plugin-docs) 
```shell
make doc
```

## Deploying from Terraform private registry
- `terraform login`, provide the token for the private registry when prompted
- `export TF_VAR_account_id=<your-account-id>`
- `export TF_VAR_auth_token=<your-auth-token>`
- Create a Terraform configuration file similar to the one provided in `examples/private_registry/read_replica.tf`
- `terraform init`
- `terraform apply`
- When the resources created are no longer needed, `terraform destroy`