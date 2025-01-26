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
	PITR               FeatureFlag = "PITR"
	PITR_RESTORE       FeatureFlag = "PITR_RESTORE"
	PITR_CLONE         FeatureFlag = "PITR_CLONE"
)

var flagEnabled = map[FeatureFlag]bool{
	CONNECTION_POOLING: false,
	DR:                 false,
	PITR:               false,
	PITR_RESTORE:       false,
	PITR_CLONE:         false,
}

func (f FeatureFlag) String() string {
	return string(f)
}

func IsFeatureFlagEnabled(featureFlag FeatureFlag) bool {
	envVarName := "YBM_FF_" + featureFlag.String()
	return strings.ToLower(os.Getenv(envVarName)) == "true" || flagEnabled[featureFlag]
}
