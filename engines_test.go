package raijincrawl

import "testing"

func TestEngines(t *testing.T) {
	m, err := Engines()
	if err != nil {
		t.Fatal(err)
	}
	// raijin-crawl runs one engine.
	e, ok := m["katana"]
	if !ok || e.Version == "" || e.Go == "" {
		t.Fatalf("engines.json needs katana with a version and a go module: %+v", m)
	}
	if got, want := e.Install(), "go install "+e.Go+"@v"+e.Version; got != want {
		t.Errorf("install = %q, want %q", got, want)
	}
}
