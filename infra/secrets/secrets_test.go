package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("FOO", "bar")
	got, err := Expand("hello ${ENV:FOO}!")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello bar!" {
		t.Errorf("got %q", got)
	}
}

func TestExpandEnvDefault(t *testing.T) {
	os.Unsetenv("MISSING")
	got, err := Expand("x=${ENV:MISSING:fallback}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "x=fallback" {
		t.Errorf("got %q", got)
	}
}

func TestExpandEnvUnsetErrors(t *testing.T) {
	os.Unsetenv("ALSO_MISSING")
	_, err := Expand("${ENV:ALSO_MISSING}")
	if err == nil {
		t.Error("expected error")
	}
}

func TestExpandFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "secret")
	if err := os.WriteFile(p, []byte("  s3cr3t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Expand("token=${FILE:" + p + "}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "token=s3cr3t" {
		t.Errorf("got %q", got)
	}
}

func TestExpandLeavesUnknownMarkers(t *testing.T) {
	got, err := Expand("v=${UNKNOWN:thing} and $bare")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v=${UNKNOWN:thing} and $bare" {
		t.Errorf("got %q", got)
	}
}

func TestExpandMultipleAndDollarOnly(t *testing.T) {
	t.Setenv("A", "1")
	t.Setenv("B", "2")
	got, err := Expand("a=${ENV:A},b=${ENV:B},c=${ENV:A}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a=1,b=2,c=1" {
		t.Errorf("got %q", got)
	}
}

func TestExpandUnclosedMarkerLeftIntact(t *testing.T) {
	got, err := Expand("oops ${ENV:X")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "${ENV:X") {
		t.Errorf("got %q", got)
	}
}

func TestRegisterCustomResolver(t *testing.T) {
	Register("REV", func(arg string) (string, error) {
		out := []byte(arg)
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		return string(out), nil
	})
	got, err := Expand("${REV:hello}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "olleh" {
		t.Errorf("got %q", got)
	}
}
