package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArgumentErrorsDoNotEchoInput(t *testing.T) {
	for _, args := range [][]string{
		nil, {"private-command-SENTINEL"}, {"check"},
		{"check", "--private-flag=SENTINEL"},
		{"check", "--duplicate-scope=SENTINEL", "private-manifest"},
		{"check", "one", "two"}, {"--version", "extra"},
	} {
		var stdout, stderr bytes.Buffer
		if got := run(args, &stdout, &stderr); got != 2 {
			t.Errorf("args %q: exit=%d, expected 2", args, got)
		}
		if strings.Contains(stdout.String()+stderr.String(), "SENTINEL") {
			t.Error("argument contents leaked")
		}
	}
}

func TestHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"check", "--help"}, {"--version"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 || stdout.Len() == 0 || stderr.Len() != 0 {
			t.Errorf("args %q: code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("synthetic broken pipe") }

func TestOutputWriteErrors(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "expected.json")
	if err := os.WriteFile(manifest, []byte(`{"version":1,"files":["one.xml"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "one.xml"), []byte(`<testsuite tests="1"><testcase name="one"/></testsuite>`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--version"}, {"check", "--json", manifest}, {"check", manifest}} {
		var stderr bytes.Buffer
		if code := run(args, brokenWriter{}, &stderr); code != 2 {
			t.Errorf("broken stdout: exit=%d", code)
		}
	}
}
