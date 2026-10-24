package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if !strings.Contains(errb.String(), "TGT-LOCK") || !strings.Contains(errb.String(), "raijin-crawl v"+version) {
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

func TestUsageErrorsExitOne(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--input", "../../testdata/sample.jsonl"}, &out, &errb); code != 1 {
		t.Errorf("no --scope: exit %d, want 1", code)
	}
	if code := run([]string{"--scope", scopeFile(t), "-u", "https://out.of-scope.net/"}, &out, &errb); code != 1 {
		t.Errorf("no target in scope: exit %d, want 1", code)
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
