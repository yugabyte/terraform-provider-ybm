package main

import (
	"context"
	"terraform-provider-ybm/managed"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

func main() {
	providerserver.Serve(context.Background(), managed.New, providerserver.ServeOpts{
		Address: "registry.terraform.io/yugabyte/ybm",
	})
}
