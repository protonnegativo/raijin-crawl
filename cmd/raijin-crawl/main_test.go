package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	raijincrawl "github.com/protonnegativo/raijin-crawl"
)

func scopeFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "scope.txt")
	if err := os.WriteFile(p, []byte("*.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNoBannerByDefault(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"--scope", scopeFile(t), "--input", "../../testdata/sample.jsonl"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if errb.Len() != 0 {
		t.Errorf("stderr should be empty without --banner, got %q", errb.String())
	}
	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
}

func TestBannerGoesToStderrOnly(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"--banner", "--scope", scopeFile(t), "--input", "../../testdata/sample.jsonl"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errb.String(), "TGT-LOCK") || !strings.Contains(errb.String(), "raijin-crawl "+strings.TrimPrefix(version(), "v")) {
		t.Errorf("banner missing or without the version: %q", errb.String())
	}
	if strings.Contains(out.String(), "TGT-LOCK") {
		t.Error("banner leaked into stdout")
	}
	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("stdout is not JSON with --banner: %v", err)
	}
}

func TestCompactByDefault(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--scope", scopeFile(t), "--input", "../../testdata/sample.jsonl"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if n := strings.Count(out.String(), "\n"); n != 1 {
		t.Errorf("default JSON should be one line, got %d lines", n)
	}
	var pretty bytes.Buffer
	if code := run([]string{"--pretty", "--scope", scopeFile(t), "--input", "../../testdata/sample.jsonl"}, &pretty, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(pretty.String(), "\n  \"tool\"") {
		t.Errorf("--pretty should indent: %q", pretty.String())
	}
	var a, b map[string]any
	if json.Unmarshal(out.Bytes(), &a) != nil || json.Unmarshal(pretty.Bytes(), &b) != nil || len(a) != len(b) {
		t.Error("compact and pretty should carry the same fields")
	}
}

func TestMissingEngineExitsTwoWithHint(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"--scope", scopeFile(t), "-u", "https://a.example.com/", "--katana", "/nonexistent/katana"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	var v struct {
		Errors []struct{ Kind, Hint string }
	}
	if err := json.Unmarshal(out.Bytes(), &v); err != nil || len(v.Errors) != 1 || v.Errors[0].Kind != "missing_engine" || !strings.Contains(v.Errors[0].Hint, "--katana") {
		t.Errorf("stdout = %s", out.String())
	}
}

func TestUsageErrorsExitOneWithoutJSON(t *testing.T) {
	for name, args := range map[string][]string{
		"no --scope": {"--input", "../../testdata/sample.jsonl"},
		"bad flag":   {"--no-such-flag"},
		"no target":  {"--scope", scopeFile(t), "-u", "https://out.of-scope.net/"},
	} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 1 {
			t.Errorf("%s: exit %d, want 1", name, code)
		}
		if out.Len() != 0 {
			t.Errorf("%s: usage errors print no JSON, got %.80s", name, out.String())
		}
	}
}

func TestScopeNamesNoSingleHostExitsTwoWithJSON(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--scope", scopeFile(t)}, &out, &errb); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.HasPrefix(out.String(), `{"schema_version":1,`) {
		t.Errorf("not a result: %.80s", out.String())
	}
}

func TestSummaryAndOrigin(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--scope", scopeFile(t), "--input", "../../testdata/sample.jsonl"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	s := out.String()
	if !strings.Contains(s, `"summary":"18 URLs, 3 forms, 17 routes: 11 findings (1 high, 4 medium, 6 low), 6 groups","origin":"https://app.example.com"`) {
		t.Errorf("summary: %s", s)
	}
	if !strings.Contains(s, `"/product?id=2&ref=home"`) || strings.Contains(s, `\u0026`) {
		t.Errorf("& in URLs should not be escaped: %s", s)
	}
	if !strings.Contains(s, `"omitted":{"duplicates":1,"out_of_scope":1}`) {
		t.Errorf("omitted: %s", s)
	}
}

func TestTableFormat(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--format", "table", "--scope", scopeFile(t), "--input", "../../testdata/sample.jsonl"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"HIGH     file /.env 200 (1)", "MEDIUM   login,params,form POST /login ?password,user (1)", "group    /user/{id} 200 (3)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("table should contain %q:\n%s", want, out.String())
		}
	}
}

func TestBudgetIsCountedAndPointsToRaw(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--max-bytes", "900", "--scope", scopeFile(t), "--input", "../../testdata/sample.jsonl"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if out.Len() > 901 {
		t.Errorf("output is %d bytes", out.Len())
	}
	var v struct {
		Omitted map[string]int
		Next    []string
		Budget  struct{ Truncated bool }
	}
	if err := json.Unmarshal(out.Bytes(), &v); err != nil || !v.Budget.Truncated || v.Omitted["groups"] == 0 || len(v.Next) != 1 || !strings.Contains(v.Next[0], "sample.jsonl") {
		t.Errorf("stdout = %s", out.String())
	}
}

// With only --scope, every host the scope names is crawled and wildcards get a next step.
func TestScopeAloneCrawlsWhatTheScopeNames(t *testing.T) {
	dir := t.TempDir()
	scopePath := filepath.Join(dir, "scope.txt")
	if err := os.WriteFile(scopePath, []byte("example.com\n*.example.com\n10.0.0.0/30\n!10.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := filepath.Join(dir, "targets.txt")
	fake := filepath.Join(dir, "katana")
	// the fake answers -version with the tested version (from engines.json), so it gets no engine_version warning
	e, _ := raijincrawl.Engines()
	script := "#!/bin/sh\n[ \"$1\" = -version ] && { echo '[INF] Current version: v" + e["katana"].Version + "' >&2; exit 0; }\nwhile [ $# -gt 0 ]; do [ \"$1\" = -list ] && cp \"$2\" " + got + "; shift; done\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := run([]string{"--scope", scopePath, "--katana", fake, "--raw", filepath.Join(dir, "raw.jsonl")}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s %s", code, errb.String(), out.String())
	}
	b, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(strings.Fields(string(b)), ",") != "example.com,10.0.0.0,10.0.0.2,10.0.0.3" {
		t.Errorf("katana got targets %q", b)
	}
	var res struct {
		Next   []string
		Errors []struct{ Kind string }
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Errors {
		if e.Kind == "engine_version" {
			t.Errorf("engine_version warning for the tested katana: %s", out.String())
		}
	}
	if len(res.Next) != 2 || !strings.Contains(res.Next[0], "raijin-subs --scope "+scopePath+" -d example.com") || !strings.Contains(res.Next[1], "raijin-probe --scope "+scopePath) {
		t.Errorf("next = %v", res.Next)
	}
}

func TestInvalidScopeStopsBeforeAnyEngine(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "scope.txt")
	rules := "example.com\nexample.com:443\n10.0.0.7/24\n"
	if err := os.WriteFile(p, []byte(rules), 0o600); err != nil {
		t.Fatal(err)
	}
	// --katana points at a file that does not exist: the scope error must come first.
	var out, errb bytes.Buffer
	code := run([]string{"--scope", p, "--katana", filepath.Join(dir, "no-such-katana"), "-u", "https://example.com/"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.HasPrefix(out.String(), `{"schema_version":1,"tool":"raijin-crawl"`) {
		t.Errorf("not a result: %s", out.String())
	}
	var res struct {
		Summary string `json:"summary"`
		Errors  []struct {
			Kind string `json:"kind"`
			Hint string `json:"hint"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) != 2 {
		t.Fatalf("want one error per invalid line, got %+v", res.Errors)
	}
	for i, want := range []string{"line 2: port in rule 'example.com:443'", "line 3: CIDR has host bits set '10.0.0.7/24'"} {
		if res.Errors[i].Kind != "scope" || !strings.Contains(res.Errors[i].Hint, want) {
			t.Errorf("error %d = %+v, want kind scope and hint containing %q", i, res.Errors[i], want)
		}
	}
	for _, e := range res.Errors {
		if e.Kind == "missing_engine" {
			t.Error("the engine must not run on an invalid scope")
		}
	}
}

func TestMissingScopeFileIsAScopeError(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"--scope", filepath.Join(t.TempDir(), "nope.txt"), "--input", "../../testdata/sample.jsonl"}, &out, &errb)
	if code != 2 || !strings.Contains(out.String(), `"kind":"scope"`) || !strings.Contains(out.String(), "cannot read the scope file") {
		t.Errorf("exit %d: %s", code, out.String())
	}
}

func TestResultStartsWithSchemaVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--scope", scopeFile(t), "--input", "../../testdata/sample.jsonl"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasPrefix(out.String(), `{"schema_version":1,"tool":"raijin-crawl",`) {
		t.Errorf("schema_version must be the first field: %.80s", out.String())
	}
}

func TestStateMarksNewOnlyOnce(t *testing.T) {
	st := filepath.Join(t.TempDir(), "state.json")
	args := []string{"--scope", scopeFile(t), "--input", "../../testdata/sample.jsonl", "--state", st}
	var first, second, errb bytes.Buffer
	if code := run(args, &first, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if code := run(args, &second, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(first.String(), `"new":true`) || !strings.Contains(first.String(), "11 new") {
		t.Errorf("first run should mark every finding new: %s", first.String())
	}
	if strings.Contains(second.String(), `"new":true`) || !strings.Contains(second.String(), "0 new") {
		t.Errorf("second run should mark none: %s", second.String())
	}
}
