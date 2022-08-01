# CI/CD Pipeline for YubgayteDB Managed Terraform Provider

## Merging a Pull Request

For every pull request, the following checks are run before it is allowed to merge to the master branch:
- Lint check
- Compile/Build
- Unit Tests
- The checks above will be automated using Github actions. 

## Promotion Plan

### Terraform Provider Promotion

The YugabyteDB Managed Terraform Provider repository will have two branches *main* and *prod*. Whenever we plan to promote the Terraform provider from the main branch to prod branch, we would run the following tests:
- Lint check
- Compile/Build
- Unit Tests
- Acceptance Tests
- Other Integration Tests from QA

The acceptance tests and integration tests will be run using the YugabyteDB Managed Terraform Provider from the main branch and will run against YugabyteDB Managed in *prod*(cloud.yugabyte.com) by creating a test account. It will be ensured that all the dangling resources are deleted from YugabyteDB Managed in case of test failures.

### Acceptance Tests

Acceptance tests are end-to-end tests where the infrastructure specified in the Terraform configuration files are actually created. Creating real infrastructure in tests verifies the described behavior of Terraform Plugins in real world use cases against the actual APIs, and verifies both local state and remote values match. Acceptance tests require a network connection and often require credentials to access an account for the given API. When writing and testing plugins, it is highly recommended to use an account dedicated to testing, to ensure no infrastructure is created in error in any environment that cannot be completely and safely destroyed.
More information about the Terraform Provider Acceptance Testing can be found [here](https://www.terraform.io/plugin/sdkv2/testing/acceptance-tests).

### API Server Promotion

Whenever the API server is promoted from *portal* branch to *prod* branch or from *dev* to *portal*, the SIT/LIT must also include running the acceptance and integration tests of the latest YugabyteDB Managed Terraform Provider *prod* version. 

 
## Developer Testing (Manual)

During the development of the YugabyteDB Managed Terraform Provider, it can be tested against a locally deployed API server or against devcloud.yugabyte.com.