package desktop

import (
	"strings"
	"testing"
)

func TestParseBridge(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"hide", ActHidePanel, true},
		{"pin", ActPinPanel, true},
		{"app:/ui/agents", "open:/ui/agents", true},
		{"app:/ui/today?d=2026-11-16", "open:/ui/today?d=2026-11-16", true},
		{"url:https://example.com/a?b=c", "url:https://example.com/a?b=c", true},
		{"url:http://example.com", "url:http://example.com", true},
		// Everything else is ignored.
		{"", "", false},
		{"quit", "", false},
		{"HIDE", "", false},
		{"hide ", "", false},
		{"app:/etc/passwd", "", false},
		{"app:ui/today", "", false},
		{"app:/ui//evil.example/x", "", false},
		{`app:/ui/..\x`, "", false},
		{"app:/ui/a b", "", false},
		{"app:/ui/x\nswitch:acme", "", false},
		{"url:file:///c:/windows/system32/calc.exe", "", false},
		{"url:javascript:alert(1)", "", false},
		{"url:mailto:a@b.c", "", false},
		{"url:https://user:pw@example.com/", "", false},
		{"url:https:///nohost", "", false},
		{"url:ftp://example.com/", "", false},
		{"url:", "", false},
		{"switch:acme", "", false},
		{"open:/ui/today", "", false},
		{"quit\x00", "", false},
		{"url:https://example.com/" + strings.Repeat("a", 3000), "", false},
	}
	for _, tt := range tests {
		name := tt.in
		if len(name) > 40 {
			name = name[:40]
		}
		t.Run(name, func(t *testing.T) {
			got, ok := ParseBridge(tt.in)
			if ok != tt.wantOK || got.Action != tt.want {
				t.Errorf("ParseBridge(%q) = %q, %v; want %q, %v", name, got.Action, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestOriginOf(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:7878/ui/panel":   "http://127.0.0.1:7878",
		"HTTP://127.0.0.1:7878":            "http://127.0.0.1:7878",
		"https://Example.com/x?y=1":        "https://example.com",
		"http://user:pw@127.0.0.1:1/":      "",
		"file:///c:/windows/win.ini":       "",
		"javascript:alert(1)":              "",
		"data:text/html,<b>x":              "",
		"":                                 "",
		"/ui/today":                        "",
		"http://":                          "",
		"http://127.0.0.1:7878@evil.test/": "",
	}
	for in, want := range cases {
		if got := OriginOf(in); got != want {
			t.Errorf("OriginOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNavigationAllowed(t *testing.T) {
	const origin = "http://127.0.0.1:7878"
	cases := []struct {
		origin, target string
		want           bool
	}{
		{origin, "http://127.0.0.1:7878/ui/week", true},
		{origin, "http://127.0.0.1:7878", true},
		{origin, "about:blank", true},
		{"", "about:blank", true},
		{"", "http://127.0.0.1:7878/ui/week", false},
		{origin, "http://127.0.0.1:7879/ui/week", false},
		{origin, "https://127.0.0.1:7878/ui/week", false},
		{origin, "http://localhost:7878/ui/week", false},
		{origin, "https://example.com/", false},
		{origin, "http://127.0.0.1:7878@evil.test/", false},
		{origin, "file:///c:/windows/win.ini", false},
		{origin, "javascript:alert(1)", false},
		{origin, "data:text/html,<b>x</b>", false},
		{origin, "", false},
		{origin, "about:blank#x", false},
	}
	for _, c := range cases {
		if got := NavigationAllowed(c.origin, c.target); got != c.want {
			t.Errorf("NavigationAllowed(%q, %q) = %v, want %v", c.origin, c.target, got, c.want)
		}
	}
}
