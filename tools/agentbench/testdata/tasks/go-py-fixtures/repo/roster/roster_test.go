package roster

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestFixtures(t *testing.T) {
	f, err := os.Open("testdata/roster.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("testdata/expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var want []Member
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %+v\nwant %+v", got, want)
	}
}
