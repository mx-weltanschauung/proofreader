package api

import (
	"net/http/httptest"
	"testing"
)

func TestParseIDList(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		want  []int64
		valid bool
	}{
		{"нет параметра", "/api/search?q=xx", nil, true},
		{"пустая строка", "/api/search?q=xx&works=", nil, true},
		{"один", "/api/search?q=xx&works=13", []int64{13}, true},
		{"несколько", "/api/search?q=xx&works=13,14", []int64{13, 14}, true},
		{"повтор схлопывается", "/api/search?q=xx&works=13,13", []int64{13}, true},
		{"мусор", "/api/search?q=xx&works=13,ы", nil, false},
		{"ноль и минус отвергаются", "/api/search?q=xx&works=0", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", c.url, nil)
			got, ok := parseIDList(req, "works")
			if ok != c.valid {
				t.Fatalf("valid = %v, want %v", ok, c.valid)
			}
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			}
		})
	}
}
