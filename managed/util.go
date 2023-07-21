/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import "github.com/hashicorp/terraform-plugin-framework/types"

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

func isDiskIopsValid(cloudType string, clusterTier string, diskIops int64) (bool, string) {
	err := ""
	if cloudType != "AWS" {
		err = "Custom Disk IOPS is only supported for AWS"
		return false, err
	}
	if clusterTier != "PAID" {
		if diskIops != 3000 {
			err = "Custom Disk IOPS is only supported for PAID tier"
			return false, err
		}
	} else {
		if diskIops%1000 != 0 {
			err = "Disk IOPS must be a multiple of 1000"
			return false, err
		}
		if diskIops < 3000 || diskIops > 16000 {
			err = "Disk IOPS must be between 3000 and 16000 (inclusive)"
			return false, err
		}
	}
	return true, err
}

// Inspired from here:
// https://stackoverflow.com/questions/37562873/most-idiomatic-way-to-select-elements-from-an-array-in-golang
// This allows us to filter a slice of any type using a function that returns a bool
func Filter[T any](ss []T, test func(T) bool) (ret []T) {
	for _, s := range ss {
		if test(s) {
			ret = append(ret, s)
		}
	}
	return
}

func SliceTypesStringToSliceString(slice []types.String) []string {
	var result []string
	for _, s := range slice {
		result = append(result, s.Value)
	}
	return result
}

func SliceStringToSliceTypesString(slice []string) []types.String {
	var result []types.String
	for _, s := range slice {
		result = append(result, types.String{Value: s})
	}
	return result
}
