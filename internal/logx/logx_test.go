package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	got := Redact("super secret clipboard text")
	if got != "len=27" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "secret") {
		t.Fatal("leaked content")
	}
}

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stkr.log")
	r, err := OpenRotating(path, 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 60) + "\n"
	for i := 0; i < 10; i++ {
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("want 3 files (current + 2 rotated), got %d", len(entries))
	}
	for _, e := range entries {
		fi, _ := e.Info()
		if fi.Size() > 100 {
			t.Errorf("%s is %d bytes", e.Name(), fi.Size())
		}
	}
}

func TestNewLogsJSONAndNeverContent(t *testing.T) {
	dir := t.TempDir()
	log, closer, err := New(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	secret := "my private idea text"
	log.Info("captured idea", "idea", Redact(secret), "id", 7)
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "stkr.log"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"msg":"captured idea"`) || !strings.Contains(s, "len=20") {
		t.Fatalf("unexpected log: %s", s)
	}
	if strings.Contains(s, secret) {
		t.Fatal("log leaked content")
	}
}
