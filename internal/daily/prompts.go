package daily

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"text/template"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
)

//go:embed templates/*.tmpl
var defaultTemplates embed.FS

// Template file names. A file with the same name in the prompts directory
// replaces the embedded default.
const (
	StartDayTemplate = "start-day.md.tmpl"
	CloseDayTemplate = "close-day.md.tmpl"
)

// StartDayData is what the start-day template can use.
type StartDayData struct {
	Date    string // YYYY-MM-DD
	Weekday string
	Digest  string
}

// CloseDayData is what the close-day template can use.
type CloseDayData struct {
	Date    string
	Weekday string
	Context string
}

const maxTemplateBytes = 64 << 10

// Render executes the named template. A file in dir (when dir is not empty)
// wins over the embedded default; a broken user template is an error that
// names the file.
func Render(dir, name string, data any) (string, error) {
	src, user, err := load(dir, name)
	if err != nil {
		return "", err
	}
	t, err := template.New(name).Option("missingkey=error").Parse(src)
	if err != nil {
		return "", templateError(name, user, dir, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", templateError(name, user, dir, err)
	}
	return buf.String(), nil
}

func templateError(name string, user bool, dir string, err error) error {
	where := "built-in template " + name
	if user {
		where = filepath.Join(dir, name)
	}
	return domain.New(domain.ErrInvalid, fmt.Sprintf("%s: %v (delete the file to go back to the default)", where, err))
}

func load(dir, name string) (src string, user bool, err error) {
	if dir != "" {
		b, rerr := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // name is one of our two constant template names
		switch {
		case rerr == nil:
			if len(b) > maxTemplateBytes {
				return "", true, domain.New(domain.ErrInvalid, filepath.Join(dir, name)+" is larger than 64 KiB")
			}
			return string(b), true, nil
		case !errors.Is(rerr, fs.ErrNotExist):
			return "", true, fmt.Errorf("read template: %w", rerr)
		}
	}
	b, err := defaultTemplates.ReadFile("templates/" + name)
	if err != nil {
		return "", false, fmt.Errorf("unknown template %q: %w", name, err)
	}
	return string(b), false, nil
}

// InitPrompts copies the default templates into dir so they can be edited.
// Existing files are kept unless force is set. It returns the paths written.
func InitPrompts(dir string, force bool) ([]string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create prompts dir: %w", err)
	}
	var written []string
	for _, name := range []string{StartDayTemplate, CloseDayTemplate} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil && !force {
			continue
		}
		b, err := defaultTemplates.ReadFile("templates/" + name)
		if err != nil {
			return written, err
		}
		if err := config.WriteFileAtomic(p, b, 0o600); err != nil {
			return written, err
		}
		written = append(written, p)
	}
	return written, nil
}
