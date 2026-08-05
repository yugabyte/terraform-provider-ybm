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
	DR                   FeatureFlag = "DR"
	GCPBackupReplication FeatureFlag = "GCP_BACKUP_REPLICATION"
	MultiZoneSupport     FeatureFlag = "MULTI_ZONE_SUPPORT"
	MultiCloudSupport    FeatureFlag = "MULTI_CLOUD_SUPPORT"
	Autoscaling          FeatureFlag = "AUTOSCALING"
)

var flagEnabled = map[FeatureFlag]bool{
	DR:                   false,
	GCPBackupReplication: false,
	MultiZoneSupport:     false,
	MultiCloudSupport:    false,
	Autoscaling:          false,
}

func (f FeatureFlag) String() string {
	return string(f)
}

func IsFeatureFlagEnabled(featureFlag FeatureFlag) bool {
	envVarName := "YBM_FF_" + featureFlag.String()
	return strings.ToLower(os.Getenv(envVarName)) == "true" || flagEnabled[featureFlag]
}
