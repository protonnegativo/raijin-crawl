package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDerivableFieldsAreLeftOut(t *testing.T) {
	res := Result{
		Tool:     "raijin-crawl",
		Findings: []Finding{{ID: "403-ab12cd34", Severity: "low", Tags: []string{"auth"}, Endpoint: Endpoint{Route: "/admin/", Status: 403}, URLs: 1, Fingerprint: "fp"}},
		Groups:   []Group{{ID: "200-ef56ab78", Endpoint: Endpoint{Route: "/about", Status: 200}, URLs: 1}},
		Omitted:  map[string]int{},
		Errors:   []Error{},
		Next:     []string{},
		Budget:   Budget{MaxItems: 20, MaxBytes: 8000},
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"fingerprint", "count", "urls", "confidence", "type", "params", "enctype", "via", "error", "locations", "new", "origin", "omitted", "errors", "next", "raw", "budget"} {
		if strings.Contains(string(b), `"`+k+`"`) {
			t.Errorf("%s should be left out: %s", k, b)
		}
	}
	if strings.Contains(string(b), "null") {
		t.Errorf("no null values: %s", b)
	}
}

func TestEndpointFieldsAreInline(t *testing.T) {
	f := Finding{ID: "200-x", Severity: "low", Tags: []string{"params"}, Endpoint: Endpoint{Route: "/product", Status: 200, Params: []string{"id", "tab"}}, Locations: []string{"/product?id=[1-40]&tab=reviews"}, URLs: 40}
	b, _ := Marshal(f)
	want := `{"id":"200-x","severity":"low","tags":["params"],"route":"/product","status":200,"params":["id","tab"],"locations":["/product?id=[1-40]&tab=reviews"]}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}

func TestCountAndBudgetAppearWhenSomethingIsCut(t *testing.T) {
	res := Result{
		Findings: []Finding{{ID: "200-x", Tags: []string{"params"}, Locations: []string{"/a?x=1"}, Count: 7, URLs: 7}},
		Groups:   []Group{{ID: "200-y", Count: 4, URLs: 4}},
		Budget:   Budget{MaxItems: 20, MaxBytes: 8000, Truncated: true},
	}
	b, _ := json.Marshal(res)
	s := string(b)
	if !strings.Contains(s, `"locations":["/a?x=1"],"count":7`) || !strings.Contains(s, `{"id":"200-y","route":"","count":4}`) {
		t.Errorf("count must be written when locations were cut: %s", s)
	}
	if !strings.Contains(s, `"budget":{"max_items":20,"max_bytes":8000,"truncated":true}`) || strings.Count(s, "budget") != 1 {
		t.Errorf("budget must be written once when something was cut: %s", s)
	}
	if strings.Contains(s, `&`) {
		t.Errorf("& must not be escaped: %s", s)
	}
}
