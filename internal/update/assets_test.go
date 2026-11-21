package update

import (
	"archive/zip"
	"bytes"
	"testing"
)

// A Windows release has the zip and the setup.exe for each architecture. The
// updater's archive path must take the zip whatever order the API lists them.
func TestAssetForIgnoresSetupFiles(t *testing.T) {
	rel := Release{Tag: "v0.2.0"}
	for _, n := range []string{
		"stkr_0.2.0_windows_amd64_setup.exe",
		"stkr_0.2.0_windows_arm64_setup.exe",
		"checksums.txt",
		"stkr_0.2.0_windows_arm64.zip",
		"stkr_0.2.0_windows_amd64.zip",
		"stkr_0.2.0_linux_amd64.tar.gz",
	} {
		rel.Assets = append(rel.Assets, Asset{Name: n, URL: "https://example.invalid/" + n})
	}
	tests := []struct {
		goos, goarch, want string
	}{
		{"windows", "amd64", "stkr_0.2.0_windows_amd64.zip"},
		{"windows", "arm64", "stkr_0.2.0_windows_arm64.zip"},
		{"linux", "amd64", "stkr_0.2.0_linux_amd64.tar.gz"},
	}
	for _, tc := range tests {
		u := &Updater{GOOS: tc.goos, GOARCH: tc.goarch}
		a, err := u.assetFor(rel)
		if err != nil || a.Name != tc.want {
			t.Errorf("%s/%s: got %q, %v; want %q", tc.goos, tc.goarch, a.Name, err, tc.want)
		}
	}
	// With only the setup file present there is no archive to install from.
	only := Release{Tag: "v0.2.0", Assets: []Asset{{Name: "stkr_0.2.0_windows_amd64_setup.exe"}}}
	if _, err := (&Updater{GOOS: "windows", GOARCH: "amd64"}).assetFor(only); err == nil {
		t.Error("a setup.exe alone must not be chosen as the archive")
	}
}

// The Windows zip now holds stkrw.exe too; the updater still takes stkr.exe.
func TestExtractBinarySkipsDesktopProgram(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct{ name, data string }{{"stkrw.exe", "desktop"}, {"stkr.exe", "cli"}} {
		w, _ := zw.Create(f.name)
		_, _ = w.Write([]byte(f.data))
	}
	_ = zw.Close()
	got, err := extractBinary(buf.Bytes(), "stkr_0.2.0_windows_amd64.zip")
	if err != nil || string(got) != "cli" {
		t.Fatalf("got %q, %v", got, err)
	}
}
