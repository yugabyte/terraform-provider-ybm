package managed

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
			gotResponse := areListsEqual(testCase.FirstList, testCase.SecondList)
			if gotResponse != testCase.ExpectedResponse {
				t.Errorf("areListsEqual(%v,%v) = %v; want %v", testCase.FirstList, testCase.SecondList, gotResponse, testCase.ExpectedResponse)
			}
		})
	}
}
