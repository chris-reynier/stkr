// Package config loads, validates, saves and redacts the STKR configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // embedded zone database so timezone works on any machine

	"gopkg.in/yaml.v3"

	"github.com/chris-reynier/stkr/internal/domain"
)

// Config is the contents of config.yaml. Fields tagged secret:"true" are
// masked by Redacted.
type Config struct {
	Port                 int       `yaml:"port"`
	Timezone             string    `yaml:"timezone"`
	DayStart             string    `yaml:"day_start"`
	DayEnd               string    `yaml:"day_end"`
	CloseReminder        string    `yaml:"close_reminder"`
	IdleGapMinutes       int       `yaml:"idle_gap_minutes"`
	RoundingHours        float64   `yaml:"rounding_hours"`
	OneDriveDir          string    `yaml:"onedrive_dir"`
	PhoneInbox           string    `yaml:"phone_inbox"`
	OpenNotebookOnSwitch bool      `yaml:"open_notebook_on_switch"`
	MicDevice            string    `yaml:"mic_device"`
	FFmpeg               string    `yaml:"ffmpeg"`
	Whisper              Whisper   `yaml:"whisper"`
	AI                   AI        `yaml:"ai"`
	ClaudeCode           Agent     `yaml:"claude_code"`
	GitHubCopilot        Agent     `yaml:"github_copilot"`
	Ollama               Ollama    `yaml:"ollama"`
	MCP                  MCP       `yaml:"mcp"`
	AgentWindowTitle     string    `yaml:"agent_window_title"`
	Launch               Launch    `yaml:"launch"`
	Update               Update    `yaml:"update"`
	Bookmarks            Bookmarks `yaml:"bookmarks"`
	AllowedOrigins       []string  `yaml:"allowed_origins"`
}

// Whisper configures the local speech-to-text binary.
type Whisper struct {
	Bin   string `yaml:"bin"`
	Model string `yaml:"model"`
}

// AI selects the AI provider.
type AI struct {
	Provider       string `yaml:"provider"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	// BatchMinutes is how often serve sorts pending ideas, for every provider.
	BatchMinutes int `yaml:"batch_minutes"`
	// PlanStartDay adds an AI suggestion to the Start Day prompt (5 s budget).
	PlanStartDay bool `yaml:"plan_start_day"`
}

// Agent configures an exec-based AI provider.
type Agent struct {
	Enabled     bool     `yaml:"enabled"`
	Bin         string   `yaml:"bin"`
	Args        []string `yaml:"args"`
	WindowTitle string   `yaml:"window_title"`
}

// Ollama configures the Ollama HTTP provider.
type Ollama struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url"`
	Model   string `yaml:"model"`
	// BatchMinutes is the old name of ai.batch_minutes. It is read from old
	// files and by `config set ollama.batch_minutes`, never written.
	BatchMinutes int `yaml:"batch_minutes,omitempty"`
}

// MCP configures the MCP server.
type MCP struct {
	Enabled bool `yaml:"enabled"`
}

// Launch is the command used to open a workspace.
type Launch struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

// Bookmarks configures bookmark capture.
type Bookmarks struct {
	// FetchTitle downloads the page to read its title (2 s budget).
	FetchTitle bool `yaml:"fetch_title"`
}

// Update configures the update check.
type Update struct {
	Check         bool   `yaml:"check"`
	Channel       string `yaml:"channel"`
	IntervalHours int    `yaml:"interval_hours"`
}

// Providers lists valid values for ai.provider.
var Providers = []string{"none", "claude_code", "github_copilot", "ollama"}

// Default returns the default configuration. It contains no personal or
// employer-specific paths.
func Default() *Config {
	return &Config{
		Port:           7878,
		DayStart:       "09:00",
		DayEnd:         "17:00",
		CloseReminder:  "16:45",
		IdleGapMinutes: 90,
		RoundingHours:  0.25,
		FFmpeg:         "ffmpeg",
		Whisper:        Whisper{Bin: "whisper-cli"},
		AI:             AI{Provider: "none", TimeoutSeconds: 120, BatchMinutes: 15},
		ClaudeCode: Agent{
			Bin:  "claude",
			Args: []string{"-p", "--output-format", "json"},
		},
		GitHubCopilot: Agent{
			Bin:  "copilot",
			Args: []string{"-p", "Read the task on standard input and reply with the JSON object only.", "--silent"},
		},
		Ollama: Ollama{
			URL:   "http://127.0.0.1:11434",
			Model: "llama3.2",
		},
		MCP:       MCP{Enabled: true},
		Launch:    Launch{Command: "code", Args: []string{"{path}"}},
		Update:    Update{Check: true, Channel: "stable", IntervalHours: 24},
		Bookmarks: Bookmarks{FetchTitle: true},
	}
}

var hhmm = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// Validate checks the configuration. Errors wrap domain.ErrInvalid.
func (c *Config) Validate() error {
	var problems []string
	bad := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }

	if c.Port < 1 || c.Port > 65535 {
		bad("port must be between 1 and 65535 (got %d)", c.Port)
	}
	times := []struct{ key, val string }{
		{"day_start", c.DayStart}, {"day_end", c.DayEnd}, {"close_reminder", c.CloseReminder},
	}
	for _, tv := range times {
		if !hhmm.MatchString(tv.val) {
			bad("%s must be HH:MM (got %q)", tv.key, tv.val)
		}
	}
	if hhmm.MatchString(c.DayStart) && hhmm.MatchString(c.DayEnd) && c.DayEnd <= c.DayStart {
		bad("day_end must be later than day_start (got %s-%s)", c.DayStart, c.DayEnd)
	}
	if c.Timezone != "" {
		if _, err := time.LoadLocation(c.Timezone); err != nil {
			bad("timezone %q is not a known IANA time zone", c.Timezone)
		}
	}
	if c.IdleGapMinutes < 1 {
		bad("idle_gap_minutes must be at least 1")
	}
	if c.RoundingHours <= 0 {
		bad("rounding_hours must be greater than 0")
	}
	if !contains(Providers, c.AI.Provider) {
		bad("ai.provider must be one of %s (got %q)", strings.Join(Providers, ", "), c.AI.Provider)
	}
	if c.AI.TimeoutSeconds < 1 {
		bad("ai.timeout_seconds must be at least 1")
	}
	if c.AI.BatchMinutes < 1 {
		bad("ai.batch_minutes must be at least 1")
	}
	if c.Update.Channel != "stable" && c.Update.Channel != "beta" {
		bad("update.channel must be stable or beta (got %q)", c.Update.Channel)
	}
	if c.Update.IntervalHours < 1 {
		bad("update.interval_hours must be at least 1")
	}
	if len(problems) > 0 {
		return domain.New(domain.ErrInvalid, "invalid configuration: "+strings.Join(problems, "; "))
	}
	return nil
}

// Location returns the configured time zone (the system zone when empty).
func (c *Config) Location() (*time.Location, error) {
	if c.Timezone == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return nil, domain.New(domain.ErrInvalid, fmt.Sprintf("timezone %q is not a known IANA time zone", c.Timezone))
	}
	return loc, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Load reads path over the defaults. A missing file yields the defaults.
// Unknown keys are returned as warnings, not errors.
func Load(path string) (*Config, []string, error) {
	cfg := Default()
	data, err := os.ReadFile(path) //nolint:gosec // path is the user's own config file
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read config: %w", err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse config %s: %w", path, domain.New(domain.ErrInvalid, err.Error()))
	}
	warnings := unknownKeys(raw, "", fieldTypes(cfg))
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, nil, fmt.Errorf("parse config %s: %w", path, domain.New(domain.ErrInvalid, err.Error()))
	}
	migrateBatchMinutes(cfg, raw)
	if err := cfg.Validate(); err != nil {
		return nil, nil, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, warnings, nil
}

// Save validates and writes the configuration atomically.
func Save(path string, c *Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return WriteFileAtomic(path, data, 0o600)
}

// WriteFileAtomic writes data to a temp file in the same directory, syncs it,
// and renames it over path.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// YAML returns the configuration as YAML, with secrets masked.
func (c *Config) YAML() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c.Redacted()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// migrateBatchMinutes moves the old ollama.batch_minutes into ai.batch_minutes
// unless the file also sets the new key.
func migrateBatchMinutes(c *Config, raw map[string]any) {
	old := c.Ollama.BatchMinutes
	c.Ollama.BatchMinutes = 0
	if old < 1 {
		return
	}
	if ai, ok := raw["ai"].(map[string]any); ok {
		if _, set := ai["batch_minutes"]; set {
			return
		}
	}
	c.AI.BatchMinutes = old
}
