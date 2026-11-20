package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/chris-reynier/stkr/internal/domain"
)

func TestDefaultsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	c := Default()
	c.Port = 9000
	c.AllowedOrigins = []string{"http://a", "http://b"}
	c.ClaudeCode.Args = []string{"-p", "x"}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	if diff := cmp.Diff(c, got, cmpopts.EquateEmpty()); diff != "" {
		t.Fatalf("round trip mismatch (-want +got):\n%s", diff)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestLoadMissingGivesDefaults(t *testing.T) {
	got, _, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(Default(), got, cmpopts.EquateEmpty()); diff != "" {
		t.Fatal(diff)
	}
}

func TestLoadUnknownKeyWarns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("port: 8000\nbogus: 1\nollama:\n  nope: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 8000 {
		t.Errorf("port = %d", got.Port)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestLoadInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	for _, body := range []string{"port: 0\n", "port: [1\n", "day_start: 9am\n"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Load(path); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("body %q: err = %v, want ErrInvalid", body, err)
		}
	}
}

func TestSet(t *testing.T) {
	tests := []struct {
		key, val string
		ok       bool
		check    func(*Config) bool
	}{
		{"port", "8080", true, func(c *Config) bool { return c.Port == 8080 }},
		{"port", "99999", false, nil},
		{"port", "abc", false, nil},
		{"day_start", "08:30", true, func(c *Config) bool { return c.DayStart == "08:30" }},
		{"day_start", "25:00", false, nil},
		{"day_start", "17:00", false, nil},
		{"day_end", "08:00", false, nil},
		{"timezone", "Europe/Paris", true, func(c *Config) bool { l, err := c.Location(); return err == nil && l.String() == "Europe/Paris" }},
		{"timezone", "Mars/Olympus", false, nil},
		{"rounding_hours", "0.5", true, func(c *Config) bool { return c.RoundingHours == 0.5 }},
		{"ai.provider", "ollama", true, func(c *Config) bool { return c.AI.Provider == "ollama" }},
		{"ai.provider", "gpt", false, nil},
		{"update.check", "false", true, func(c *Config) bool { return !c.Update.Check }},
		{"update.check", "maybe", false, nil},
		{"allowed_origins", "http://a, http://b", true, func(c *Config) bool { return len(c.AllowedOrigins) == 2 && c.AllowedOrigins[1] == "http://b" }},
		{"whisper.model", "m.bin", true, func(c *Config) bool { return c.Whisper.Model == "m.bin" }},
		{"nope", "1", false, nil},
		{"ollama.nope", "1", false, nil},
		{"port.x", "1", false, nil},
		{"ollama", "x", false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.key+"="+tt.val, func(t *testing.T) {
			c := Default()
			err := c.Set(tt.key, tt.val)
			if tt.ok {
				if err != nil {
					t.Fatal(err)
				}
				if !tt.check(c) {
					t.Fatal("value not applied")
				}
				return
			}
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if diff := cmp.Diff(Default(), c, cmpopts.EquateEmpty()); diff != "" {
				t.Fatalf("config changed despite error: %s", diff)
			}
		})
	}
}

type withSecret struct {
	Name  string `yaml:"name"`
	Token string `yaml:"token" secret:"true"`
	Inner struct {
		Key string `yaml:"key" secret:"true"`
	} `yaml:"inner"`
}

func TestRedactStruct(t *testing.T) {
	v := withSecret{Name: "n", Token: "hunter2"}
	v.Inner.Key = "topsecret"
	RedactStruct(&v)
	if v.Token != RedactedValue || v.Inner.Key != RedactedValue || v.Name != "n" {
		t.Fatalf("got %+v", v)
	}
	empty := withSecret{}
	RedactStruct(&empty)
	if empty.Token != "" {
		t.Fatal("empty secret should stay empty")
	}
}

func TestYAMLDoesNotMutateOriginal(t *testing.T) {
	c := Default()
	out, err := c.YAML()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "port: 7878") {
		t.Fatalf("yaml: %s", out)
	}
	if c.Port != 7878 {
		t.Fatal("mutated")
	}
}

func TestHome(t *testing.T) {
	d := filepath.Join(t.TempDir(), "h")
	t.Setenv("STKR_HOME", d)
	got, err := ResolveHome("")
	if err != nil || got != d {
		t.Fatalf("env: %q %v", got, err)
	}
	flag := filepath.Join(t.TempDir(), "f")
	if got, _ := ResolveHome(flag); got != flag {
		t.Fatalf("flag: %q", got)
	}
	if err := EnsureHome(d); err != nil {
		t.Fatal(err)
	}
	for _, s := range subdirs {
		fi, err := os.Stat(filepath.Join(d, s))
		if err != nil || !fi.IsDir() {
			t.Fatalf("%s missing", s)
		}
		if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
			t.Errorf("%s perm = %v", s, fi.Mode().Perm())
		}
	}
	t.Setenv("STKR_HOME", "")
	if got, err := ResolveHome(""); err != nil || filepath.Base(got) != ".stkr" {
		t.Fatalf("default: %q %v", got, err)
	}
}
