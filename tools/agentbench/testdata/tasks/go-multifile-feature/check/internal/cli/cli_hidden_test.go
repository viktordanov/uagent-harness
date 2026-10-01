package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

type jsonItem struct {
	Name  string  `json:"name"`
	Qty   int     `json:"qty"`
	Price float64 `json:"price"`
}

func TestJSONHidden(t *testing.T) {
	p := writeFile(t, "pear,2,0.5\napple,3,1.20\nfig,10,12.05\n")
	for _, c := range []struct {
		args []string
		want []jsonItem
	}{
		{[]string{"-format", "json", p}, []jsonItem{{"apple", 3, 1.2}, {"fig", 10, 12.05}, {"pear", 2, 0.5}}},
		{[]string{"-format", "json", "-sort", "qty", p}, []jsonItem{{"fig", 10, 12.05}, {"apple", 3, 1.2}, {"pear", 2, 0.5}}},
		{[]string{"-sort", "qty", "-format=json", p}, []jsonItem{{"fig", 10, 12.05}, {"apple", 3, 1.2}, {"pear", 2, 0.5}}},
	} {
		var out, errb bytes.Buffer
		if code := Run(c.args, &out, &errb); code != 0 {
			t.Fatalf("%v: exit %d: %s", c.args, code, errb.String())
		}
		var got []jsonItem
		dec := json.NewDecoder(&out)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("%v: not a JSON array of items: %v", c.args, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v: got %+v, want %+v", c.args, got, c.want)
		}
	}
}

func TestJSONEmptyHidden(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Run([]string{"-format", "json", writeFile(t, "# nothing\n")}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if got := bytes.TrimSpace(out.Bytes()); string(got) != "[]" {
		t.Fatalf("empty inventory = %q, want []", got)
	}
}

func TestFormatFlagHidden(t *testing.T) {
	p := writeFile(t, "pear,2,0.5\n")
	var out, errb bytes.Buffer
	if code := Run([]string{"-format", "text", p}, &out, &errb); code != 0 || out.String() != "NAME  QTY  PRICE\npear  2    0.50\n" {
		t.Fatalf("-format text: exit %d, %q", code, out.String())
	}
	out.Reset()
	if code := Run([]string{"-format", "yaml", p}, &out, &errb); code != 2 {
		t.Fatalf("-format yaml: exit %d, want 2", code)
	}
	if out.Len() != 0 {
		t.Fatalf("-format yaml wrote to stdout: %q", out.String())
	}
}
