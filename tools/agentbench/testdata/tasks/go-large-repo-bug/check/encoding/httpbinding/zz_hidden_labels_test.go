package httpbinding

import (
	"net/http"
	"testing"
)

// The agentbench check: several labels, values longer and shorter than
// their placeholders, in every order.
func TestHiddenMultipleLabels(t *testing.T) {
	cases := []struct {
		uri     string
		labels  [][2]string
		path    string
		rawPath string
	}{
		{"/accounts/{AccountId}/vaults/{VaultName}/archives", [][2]string{{"AccountId", "-"}, {"VaultName", "photos-2024-archive"}},
			"/accounts/-/vaults/photos-2024-archive/archives", "/accounts/-/vaults/photos-2024-archive/archives"},
		{"/accounts/{AccountId}/vaults/{VaultName}/archives", [][2]string{{"AccountId", "123456789012"}, {"VaultName", "photos-2024-archive"}},
			"/accounts/123456789012/vaults/photos-2024-archive/archives", "/accounts/123456789012/vaults/photos-2024-archive/archives"},
		{"/accounts/{AccountId}/vaults/{VaultName}/archives", [][2]string{{"VaultName", "v"}, {"AccountId", "a-much-longer-account-identifier"}},
			"/accounts/a-much-longer-account-identifier/vaults/v/archives", "/accounts/a-much-longer-account-identifier/vaults/v/archives"},
		{"/{Bucket}/{Key+}?versions", [][2]string{{"Bucket", "b"}, {"Key", "2024/summer trip/IMG 01.jpg"}},
			"/b/2024/summer trip/IMG 01.jpg", "/b/2024/summer%20trip/IMG%2001.jpg"},
		{"/{A}/{B}/{C}/tail", [][2]string{{"A", "x"}, {"B", "yy"}, {"C", "a value with spaces and more"}},
			"/x/yy/a value with spaces and more/tail", "/x/yy/a%20value%20with%20spaces%20and%20more/tail"},
	}
	for _, c := range cases {
		path, _ := SplitURI(c.uri)
		e, err := NewEncoder(path, "", http.Header{})
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range c.labels {
			if err := e.SetURI(l[0]).String(l[1]); err != nil {
				t.Fatalf("%s: %v", c.uri, err)
			}
		}
		req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
		req, err = e.Encode(req)
		if err != nil {
			t.Fatal(err)
		}
		if req.URL.Path != c.path || req.URL.RawPath != c.rawPath {
			t.Errorf("%s %v:\n got  %q %q\n want %q %q", c.uri, c.labels, req.URL.Path, req.URL.RawPath, c.path, c.rawPath)
		}
	}
}
