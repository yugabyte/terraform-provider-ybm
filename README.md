# YugabyteDB Managed Terraform Provider

Terraform Provider for YugabyteDB Managed

## Build Provider

Run the following command to build the provider

```shell
make install
```

## Generate Documentation

To generate documentation

```shell
make doc
```

## Release to Terraform Public Registry

Prerelease versions are supported (available if explicitly defined but not chosen automatically) with a hyphen (-) delimiter, such as v1.2.3-pre. More information can be found [here](https://www.terraform.io/registry/providers/publishing#creating-a-github-release).