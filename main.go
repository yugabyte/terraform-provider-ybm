/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package main

import (
	"context"
	"flag"

	"github.com/yugabyte/terraform-provider-ybm/managed"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

var version = "v0.1.0"

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	providerserver.Serve(context.Background(), managed.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/yugabyte/ybm",
		Debug:   debug,
	})
}
