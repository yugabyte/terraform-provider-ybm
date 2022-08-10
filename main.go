package main

import (
	"context"
	"terraform-provider-ybm/managed"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

var version = "v0.1.0"

func main() {
	providerserver.Serve(context.Background(), managed.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/yugabyte/ybm",
	})
}
