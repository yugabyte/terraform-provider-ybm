/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package util

import (
	"testing"
)

func TestAreListsEqual(t *testing.T) {
	testCases := []struct {
		TestName         string
		FirstList        []string
		SecondList       []string
		ExpectedResponse bool
	}{
		{
			TestName:         "Equal lists",
			FirstList:        []string{"id1", "id2"},
			SecondList:       []string{"id1", "id2"},
			ExpectedResponse: true,
		},
		{
			TestName:         "Unequal lists of same size",
			FirstList:        []string{"id1", "id2"},
			SecondList:       []string{"id2", "id1"},
			ExpectedResponse: false,
		},
		{
			TestName:         "Unequal lists of different size",
			FirstList:        []string{"id1", "id2"},
			SecondList:       []string{"id1"},
			ExpectedResponse: false,
		},
		{
			TestName:         "Empty lists",
			FirstList:        []string{},
			SecondList:       []string{},
			ExpectedResponse: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.TestName, func(t *testing.T) {
			gotResponse := AreListsEqual(testCase.FirstList, testCase.SecondList)
			if gotResponse != testCase.ExpectedResponse {
				t.Errorf("AreListsEqual(%v,%v) = %v; want %v", testCase.FirstList, testCase.SecondList, gotResponse, testCase.ExpectedResponse)
			}
		})
	}
}

func TestIsDiskSizeValid(t *testing.T) {
	testCases := []struct {
		TestName         string
		ClusterTier      string
		DiskSize         int64
		ExpectedResponse bool
	}{
		{
			TestName:         "Paid Cluster - Disk size great than 50",
			ClusterTier:      "PAID",
			DiskSize:         60,
			ExpectedResponse: true,
		},
		{
			TestName:         "Paid Cluster - Disk size less than 50",
			ClusterTier:      "PAID",
			DiskSize:         40,
			ExpectedResponse: false,
		},
		{
			TestName:         "FREE Cluster",
			ClusterTier:      "FREE",
			DiskSize:         40,
			ExpectedResponse: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.TestName, func(t *testing.T) {
			gotResponse := IsDiskSizeValid(testCase.ClusterTier, testCase.DiskSize)
			if gotResponse != testCase.ExpectedResponse {
				t.Errorf("IsDiskSizeValid(%v,%v) = %v; want %v", testCase.ClusterTier, testCase.DiskSize, gotResponse, testCase.ExpectedResponse)
			}
		})
	}
}
