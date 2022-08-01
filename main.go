package main

import (
	"context"
	"yugabytedb-managed-terraform-provider/managed"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

func main() {
	providerserver.Serve(context.Background(), managed.New, providerserver.ServeOpts{
		Address: "yugabyte/managed/yugabytedb-managed",
	})
}
