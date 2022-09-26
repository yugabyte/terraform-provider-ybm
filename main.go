/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package main

import (
	"context"

	"github.com/yugabyte/terraform-provider-ybm/managed"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

var version = "v0.1.0"

func main() {
	providerserver.Serve(context.Background(), managed.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/yugabyte/ybm",
	})
}
