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

func TestGetSetAllIsSecret(t *testing.T) {
	c := Default()
	if v, ok := c.Get("ollama.url"); !ok || v != "http://127.0.0.1:11434" {
		t.Errorf("get string: %q %v", v, ok)
	}
	for key, want := range map[string]string{"port": "7878", "rounding_hours": "0.25", "mcp.enabled": "true", "claude_code.args": "-p,--output-format,json"} {
		if v, ok := c.Get(key); !ok || v != want {
			t.Errorf("Get(%s) = %q %v, want %q", key, v, ok, want)
		}
	}
	for _, key := range []string{"nope", "whisper"} {
		if _, ok := c.Get(key); ok {
			t.Errorf("Get(%s) should fail", key)
		}
	}
	if IsSecret("ollama.url") || IsSecret("nope") {
		t.Error("IsSecret false positives")
	}
	// Day start/end are only valid together; failure leaves c unchanged.
	if err := c.SetAll(map[string]string{"day_start": "20:00", "day_end": "23:00"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetAll(map[string]string{"day_end": "10:00", "ollama.model": "x"}); err == nil {
		t.Fatal("want error")
	}
	if c.DayEnd != "23:00" || c.Ollama.Model == "x" {
		t.Errorf("config changed on error: %+v", c)
	}
}

func TestBatchMinutesAlias(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "c.yaml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// The old key is read into ai.batch_minutes and never written back.
	c, warns, err := Load(write("ollama:\n  batch_minutes: 40\n"))
	if err != nil || len(warns) != 0 || c.AI.BatchMinutes != 40 || c.Ollama.BatchMinutes != 0 {
		t.Fatalf("old key: %+v %v %v", c.AI, warns, err)
	}
	// The new key wins when both are present.
	c, _, err = Load(write("ai:\n  batch_minutes: 5\nollama:\n  batch_minutes: 40\n"))
	if err != nil || c.AI.BatchMinutes != 5 {
		t.Fatalf("both keys: %+v %v", c.AI, err)
	}
	if Default().AI.BatchMinutes != 15 {
		t.Errorf("default = %d", Default().AI.BatchMinutes)
	}
	out := filepath.Join(dir, "out.yaml")
	if err := Save(out, c); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); strings.Contains(string(b), "batch_minutes: 0") || strings.Count(string(b), "batch_minutes") != 1 {
		t.Errorf("saved:\n%s", b)
	}
	// config set with the old name changes the new field.
	if err := c.Set("ollama.batch_minutes", "9"); err != nil || c.AI.BatchMinutes != 9 {
		t.Fatalf("set old name: %v %d", err, c.AI.BatchMinutes)
	}
	if err := c.Set("ai.batch_minutes", "0"); err == nil {
		t.Error("batch_minutes 0 accepted")
	}
}
