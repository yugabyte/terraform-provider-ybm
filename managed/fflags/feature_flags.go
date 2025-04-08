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
	CONNECTION_POOLING FeatureFlag = "CONNECTION_POOLING"
	DR                 FeatureFlag = "DR"
	PITR_CONFIG        FeatureFlag = "PITR_CONFIG"
	PITR_CLONE         FeatureFlag = "PITR_CLONE"
)

var flagEnabled = map[FeatureFlag]bool{
	CONNECTION_POOLING: false,
	DR:                 false,
	PITR_CONFIG:        false,
	PITR_CLONE:         false,
}

func (f FeatureFlag) String() string {
	return string(f)
}

func IsFeatureFlagEnabled(featureFlag FeatureFlag) bool {
	envVarName := "YBM_FF_" + featureFlag.String()
	return strings.ToLower(os.Getenv(envVarName)) == "true" || flagEnabled[featureFlag]
}
