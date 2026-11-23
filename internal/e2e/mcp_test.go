package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The binary speaks MCP on stdio: stdout carries only JSON-RPC messages.
func TestSmokeP08MCP(t *testing.T) {
	home := t.TempDir()
	if out, errOut, code := run(t, home, "switch", "planning"); code != 0 {
		t.Fatalf("switch: %d %s %s", code, out, errOut)
	}

	cmd := exec.Command(binPath, "mcp")
	cmd.Env = append(os.Environ(), "STKR_HOME="+home)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			_ = cmd.Process.Kill()
		}
	}()
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
		close(done)
	}()

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	send := func(s string) {
		if _, err := fmt.Fprintln(stdin, s); err != nil {
			t.Fatal(err)
		}
	}
	// recv reads the next line and requires it to be a JSON-RPC message.
	recv := func() map[string]any {
		t.Helper()
		if !sc.Scan() {
			t.Fatalf("no output from stkr mcp: %v", sc.Err())
		}
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("stdout line is not JSON: %q", sc.Text())
		}
		if m["jsonrpc"] != "2.0" {
			t.Fatalf("not a JSON-RPC message: %q", sc.Text())
		}
		return m
	}

	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	init := recv()
	info := init["result"].(map[string]any)["serverInfo"].(map[string]any)
	if init["id"].(float64) != 1 || info["name"] != "stkr" {
		t.Fatalf("initialize: %v", init)
	}
	send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	list := recv()
	tools := list["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 16 {
		t.Fatalf("want 16 tools, got %d", len(tools))
	}

	send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_now","arguments":{}}}`)
	call := recv()
	res := call["result"].(map[string]any)
	if res["isError"] == true {
		t.Fatalf("get_now failed: %v", res)
	}
	sc2 := res["structuredContent"].(map[string]any)
	if sc2["tracking"] != true || sc2["context"].(map[string]any)["id"] != "planning" {
		t.Fatalf("get_now: %v", sc2)
	}

	send(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"switch_context","arguments":{"context":"nope"}}}`)
	bad := recv()["result"].(map[string]any)
	if bad["isError"] != true || !strings.Contains(fmt.Sprint(bad["content"]), "[not_found]") {
		t.Fatalf("expected not_found tool error: %v", bad)
	}

	// Closing stdin ends the session cleanly.
	_ = stdin.Close()
	if sc.Scan() {
		t.Fatalf("unexpected extra output: %q", sc.Text())
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("stkr mcp exit: %v", err)
	}
	// The event is recorded with source mcp: nothing to check for get_now,
	// but the switch above must not have changed the running context.
	if out, _, _ := run(t, home, "now", "--json"); !strings.Contains(out, "planning") {
		t.Fatalf("now: %s", out)
	}
}

func TestMCPDisabled(t *testing.T) {
	home := t.TempDir()
	if _, _, code := run(t, home, "config", "set", "mcp.enabled", "false"); code != 0 {
		t.Fatal("config set failed")
	}
	if _, errOut, code := run(t, home, "mcp"); code != 2 || !strings.Contains(errOut, "disabled") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

// Requests that are already written when stdin closes must still be answered.
func TestMCPAnswersAfterStdinEOF(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		"",
	}, "\n")
	check := func(t *testing.T, stdout string) {
		t.Helper()
		ids := map[float64]bool{}
		for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil || m["jsonrpc"] != "2.0" {
				t.Fatalf("stdout line is not JSON-RPC: %q", line)
			}
			if id, ok := m["id"].(float64); ok {
				ids[id] = true
				if id == 2 && len(m["result"].(map[string]any)["tools"].([]any)) != 16 {
					t.Fatalf("tools/list: %v", m)
				}
			}
		}
		if !ids[1] || !ids[2] {
			t.Fatalf("missing responses, got ids %v from %q", ids, stdout)
		}
	}
	t.Run("pipe", func(t *testing.T) {
		cmd := exec.Command(binPath, "mcp")
		cmd.Env = append(os.Environ(), "STKR_HOME="+t.TempDir())
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("exit: %v", err)
		}
		check(t, string(out))
	})
	t.Run("file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "in.jsonl")
		if err := os.WriteFile(f, []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}
		in, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		cmd := exec.Command(binPath, "mcp")
		cmd.Env = append(os.Environ(), "STKR_HOME="+t.TempDir())
		cmd.Stdin = in
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("exit: %v", err)
		}
		check(t, string(out))
	})
}
