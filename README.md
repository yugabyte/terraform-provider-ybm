# YugabyteDB Aeon Terraform Provider

Terraform Provider for YugabyteDB Aeon

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

## Update Managed Client Mocks

When the managed Go client changes and the `AccountApi`, `NetworkApi`, or `ProjectApi` interfaces drift, first update the client dependency and then regenerate the GoMock files:

```shell
make update-client
make update-mock-apis
go test ./managed/...
```

The mock regeneration target uses `mockgen` in reflect mode so the generated files keep the same `github.com/yugabyte/yugabytedb-managed-go-client-internal` source banner and `openapi` import alias as the existing mocks.

## Debugging

Please read [Debugging help](./DEBUGGING.md)

## Release to Terraform Public Registry

Prerelease versions are supported (available if explicitly defined but not chosen automatically) with a hyphen (-) delimiter, such as v1.2.3-pre. More information can be found [here](https://www.terraform.io/registry/providers/publishing#creating-a-github-release).