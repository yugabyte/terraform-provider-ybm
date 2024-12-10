/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package fflags

import (
	"os"
	"strings"
)

type FeatureFlag string

const (
	GOOGLECLOUD_INTEGRATION_ENABLED FeatureFlag = "GOOGLECLOUD_INTEGRATION_ENABLED"
	DB_AUDIT_LOGGING                FeatureFlag = "DB_AUDIT_LOGGING"
	CONNECTION_POOLING              FeatureFlag = "CONNECTION_POOLING"
	DR                              FeatureFlag = "DR"
)

var flagEnabled = map[FeatureFlag]bool{
	GOOGLECLOUD_INTEGRATION_ENABLED: false,
	DB_AUDIT_LOGGING:                false,
	CONNECTION_POOLING:              false,
	DR:                              false,
}

func (f FeatureFlag) String() string {
	return string(f)
}

func IsFeatureFlagEnabled(featureFlag FeatureFlag) bool {
	envVarName := "YBM_FF_" + featureFlag.String()
	return strings.ToLower(os.Getenv(envVarName)) == "true" || flagEnabled[featureFlag]
}
