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
	S3Integration        FeatureFlag = "S3_INTEGRATION"
)

var flagEnabled = map[FeatureFlag]bool{
	DR:                   false,
	GCPBackupReplication: false,
	S3Integration:        false,
}

func (f FeatureFlag) String() string {
	return string(f)
}

func IsFeatureFlagEnabled(featureFlag FeatureFlag) bool {
	envVarName := "YBM_FF_" + featureFlag.String()
	return strings.ToLower(os.Getenv(envVarName)) == "true" || flagEnabled[featureFlag]
}
