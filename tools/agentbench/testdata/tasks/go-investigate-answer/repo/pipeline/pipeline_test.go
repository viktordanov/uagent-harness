package pipeline

import "testing"

func TestProcessBatch(t *testing.T) {
	res := ProcessBatch([]Record{
		{ID: "1", Region: " EMEA ", Amount: 1000},
		{ID: "2", Region: "na", Amount: 500},
		{ID: "", Region: "na", Amount: 5},
		{ID: "4", Region: "mars", Amount: 5},
		{ID: "5", Region: "eu", Amount: 1, Test: true},
	})
	if len(res.Groups["eu"]) != 1 || res.Groups["eu"][0].Tax != 200 || len(res.Groups["na"]) != 1 {
		t.Fatalf("groups %+v", res.Groups)
	}
	if len(res.Rejected) != 2 {
		t.Fatalf("rejected %+v", res.Rejected)
	}
}
