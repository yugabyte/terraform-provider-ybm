/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

func areListsEqual(l1 []string, l2 []string) bool {
	if len(l1) != len(l2) {
		return false
	}
	for i := range l1 {
		if l1[i] != l2[i] {
			return false
		}
	}
	return true
}

func isDiskSizeValid(clusterTier string, diskSize int64) bool {
	if clusterTier == "PAID" && diskSize < 50 {
		return false
	}
	return true
}
