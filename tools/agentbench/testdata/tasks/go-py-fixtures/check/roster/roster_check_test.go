package roster

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRolesInFixtures(t *testing.T) {
	f, err := os.Open("testdata/roster.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	roles := map[string]string{}
	for _, m := range got {
		roles[m.Name] = m.Role
	}
	want := map[string]string{"Alice": "lead", "Bob": "member", "Carol": "member", "Dmitri": "lead", "Erin": "member", "Frank": "contractor"}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	b, err := os.ReadFile("testdata/expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var exp []map[string]string
	if err := json.Unmarshal(b, &exp); err != nil {
		t.Fatal(err)
	}
	for _, m := range exp {
		if m["role"] != want[m["name"]] {
			t.Errorf("expected.json: %s has role %q", m["name"], m["role"])
		}
	}
	tsv, _ := os.ReadFile("testdata/roster.tsv")
	if !strings.Contains(string(tsv), "Frank\tweb\tcontractor\n") {
		t.Errorf("roster.tsv has no role column for Frank:\n%s", tsv)
	}
}

func TestDefaultRole(t *testing.T) {
	got, err := Load(strings.NewReader("Zed\tops\nYan\tops\t\nXi\tops\tlead\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Member{{Name: "Zed", Team: "ops", Role: "member"}, {Name: "Yan", Team: "ops", Role: "member"}, {Name: "Xi", Team: "ops", Role: "lead"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %+v, want %+v", got, want)
	}
	if _, err := Load(strings.NewReader("a\tb\tc\td\n")); err == nil {
		t.Error("four columns should be an error")
	}
}
