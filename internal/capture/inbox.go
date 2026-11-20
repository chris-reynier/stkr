package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// StableAfter is how long a file must be unchanged before the inbox takes it.
const StableAfter = 5 * time.Second

// MaxInboxFiles bounds the files handled per scan.
const MaxInboxFiles = 50

// MaxInboxText is the largest text file read (bytes).
const MaxInboxText = 64 << 10

// Tags of an idea.
const (
	TagPro      = "pro"
	TagPersonal = "personal"
)

var audioExt = map[string]bool{".m4a": true, ".mp3": true, ".wav": true, ".ogg": true, ".opus": true}

// InboxFile is a file waiting in the phone inbox.
type InboxFile struct {
	Path  string
	Audio bool // false means a text file
	Tag   string
}

// TagFor applies the phone inbox tag rules (contract phone-inbox v1): a file
// name starting with p_ or perso_ (any case), or a file inside the personal
// subfolder, is personal; everything else is pro.
func TagFor(name string, inPersonalDir bool) string {
	l := strings.ToLower(name)
	if inPersonalDir || strings.HasPrefix(l, "p_") || strings.HasPrefix(l, "perso_") {
		return TagPersonal
	}
	return TagPro
}

// ScanInbox lists the files in dir (and dir/personal) that are ready: a
// supported extension and unchanged for StableAfter relative to now. The
// processed folder is ignored. Results are ordered by modification time.
func ScanInbox(dir string, now time.Time) ([]InboxFile, error) {
	type found struct {
		f  InboxFile
		mt time.Time
	}
	var all []found
	for _, sub := range []struct {
		path     string
		personal bool
	}{{dir, false}, {filepath.Join(dir, "personal"), true}} {
		entries, err := os.ReadDir(sub.path)
		if err != nil {
			if os.IsNotExist(err) && sub.personal {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			isAudio := audioExt[ext]
			if !isAudio && ext != ".txt" {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if age := now.Sub(info.ModTime()); age < StableAfter && age > -time.Hour {
				continue // still being written
			}
			all = append(all, found{
				f:  InboxFile{Path: filepath.Join(sub.path, e.Name()), Audio: isAudio, Tag: TagFor(e.Name(), sub.personal)},
				mt: info.ModTime(),
			})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].mt.Before(all[j].mt) })
	if len(all) > MaxInboxFiles {
		all = all[:MaxInboxFiles]
	}
	out := make([]InboxFile, len(all))
	for i, a := range all {
		out[i] = a.f
	}
	return out, nil
}

// MoveProcessed renames path into dir/processed, adding -2, -3, ... before
// the extension when the name is taken. It returns the new path.
func MoveProcessed(dir, path string) (string, error) {
	pdir := filepath.Join(dir, "processed")
	if err := os.MkdirAll(pdir, 0o700); err != nil {
		return "", err
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	dst := filepath.Join(pdir, base)
	for n := 2; ; n++ {
		if _, err := os.Lstat(dst); os.IsNotExist(err) {
			break
		}
		if n > 10000 {
			return "", fmt.Errorf("no free name in %s", pdir)
		}
		dst = filepath.Join(pdir, fmt.Sprintf("%s-%d%s", stem, n, ext))
	}
	if err := os.Rename(path, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// ReadText reads a text file (at most MaxInboxText bytes), dropping a BOM and
// invalid UTF-8.
func ReadText(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from the inbox scan
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, MaxInboxText)
	n, _ := f.Read(buf)
	s := strings.TrimPrefix(string(buf[:n]), bomPrefix)
	s = strings.ToValidUTF8(s, "")
	return strings.TrimSpace(s), nil
}

const bomPrefix = "\xef\xbb\xbf"
