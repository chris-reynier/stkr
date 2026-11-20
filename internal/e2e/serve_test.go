package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// startServe runs `stkr serve --port 0` and returns its base URL and a stop
// function that shuts it down.
func startServe(t *testing.T, home string, extra ...string) (string, func() error) {
	t.Helper()
	cmd := exec.Command(binPath, append([]string{"serve", "--port", "0"}, extra...)...)
	cmd.Env = append(os.Environ(), "STKR_HOME="+home)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(out).ReadString('\n')
		lines <- line
	}()
	var base string
	select {
	case line := <-lines:
		i := strings.Index(line, "http://")
		if i < 0 {
			_ = cmd.Process.Kill()
			t.Fatalf("no URL in %q (stderr %s)", line, stderr.String())
		}
		base = strings.TrimSpace(line[i:])
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("serve did not start")
	}
	stop := func() error {
		if runtime.GOOS == "windows" {
			// Windows cannot deliver SIGINT to a child; graceful shutdown is
			// covered by the httpapi tests.
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil
		}
		_ = cmd.Process.Signal(os.Interrupt)
		return cmd.Wait()
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return base, stop
}

func call(t *testing.T, method, url string, hdr ...string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i] == "Host" {
			req.Host = hdr[i+1]
			continue
		}
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestSmokeP02Serve(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	base, stop := startServe(t, home)

	if code, b := call(t, "GET", base+"/healthz"); code != 200 || !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("healthz: %d %s", code, b)
	}
	code, b := call(t, "POST", base+"/api/switch/other", "X-STKR-Source", "deck")
	if code != 200 || !strings.Contains(string(b), `"created":true`) || !strings.Contains(string(b), `"source":"deck"`) {
		t.Fatalf("switch: %d %s", code, b)
	}
	code, b = call(t, "GET", base+"/api/now")
	var now struct {
		Tracking bool `json:"tracking"`
		Context  struct {
			ID string `json:"id"`
		} `json:"context"`
	}
	if err := json.Unmarshal(b, &now); err != nil || code != 200 || !now.Tracking || now.Context.ID != "other" {
		t.Fatalf("now: %d %s", code, b)
	}
	code, b = call(t, "GET", base+"/img/now.png")
	img, err := png.Decode(bytes.NewReader(b))
	if code != 200 || err != nil || img.Bounds().Dx() != 144 || img.Bounds().Dy() != 144 {
		t.Fatalf("now.png: %d %v", code, err)
	}
	if code, _ := call(t, "POST", base+"/api/stop", "Origin", "http://evil.com"); code != 403 {
		t.Fatalf("bad origin: %d", code)
	}
	if code, _ := call(t, "GET", base+"/api/now", "Host", "evil.com"); code != 421 {
		t.Fatalf("bad host: %d", code)
	}

	// A second server on the same port is refused with exit code 2.
	port := base[strings.LastIndex(base, ":")+1:]
	_, errOut, exit := run(t, home, "serve", "--port", port)
	if exit != 2 || !strings.Contains(errOut, "cannot listen") {
		t.Fatalf("busy port: exit=%d stderr=%q", exit, errOut)
	}

	if err := stop(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	// The event was committed: visible to the CLI after shutdown.
	out, _, code2 := run(t, home, "now")
	if code2 != 0 || !strings.Contains(out, "other") {
		t.Fatalf("now after serve: %d %q", code2, out)
	}
}
