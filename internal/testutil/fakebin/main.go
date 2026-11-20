// Command fakebin is a tiny configurable program that stands in for external
// tools (ffmpeg, whisper, claude, ...) in tests. Behaviour is set by env vars:
//
//	FAKEBIN_STDOUT     text written to stdout
//	FAKEBIN_STDERR     text written to stderr
//	FAKEBIN_EXIT       exit code (default 0)
//	FAKEBIN_SLEEP_MS   sleep before doing anything (for timeout tests)
//	FAKEBIN_ECHO_STDIN if "1", copy stdin to stdout
//	FAKEBIN_ARGS_FILE  if set, write argv (one per line) to this file
package main

import (
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	if ms, err := strconv.Atoi(os.Getenv("FAKEBIN_SLEEP_MS")); err == nil && ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
	if p := os.Getenv("FAKEBIN_ARGS_FILE"); p != "" {
		_ = os.WriteFile(p, []byte(strings.Join(os.Args[1:], "\n")), 0o600)
	}
	if os.Getenv("FAKEBIN_ECHO_STDIN") == "1" {
		_, _ = io.Copy(os.Stdout, os.Stdin)
	}
	if s := os.Getenv("FAKEBIN_STDOUT"); s != "" {
		_, _ = os.Stdout.WriteString(s)
	}
	if s := os.Getenv("FAKEBIN_STDERR"); s != "" {
		_, _ = os.Stderr.WriteString(s)
	}
	code, _ := strconv.Atoi(os.Getenv("FAKEBIN_EXIT"))
	os.Exit(code)
}
