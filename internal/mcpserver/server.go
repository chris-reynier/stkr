// Package mcpserver exposes STKR to MCP clients (Claude Code, GitHub Copilot
// in VS Code) over stdio. It is a thin adapter over service.Service.
//
// stdout belongs to the protocol: nothing in this package prints to it, and
// logging goes to the slog default (the data-dir log file).
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

// Limits on tool input.
const (
	MaxTextBytes = 10 * 1024
	MaxLimit     = 200
	DefaultLimit = 50
	maxShort     = 500
)

// ManifestURI is the resource that describes STKR to an agent.
const ManifestURI = "stkr://manifest"

// Options configure New.
type Options struct {
	Version string
	Logger  *slog.Logger
}

type adapter struct {
	tools []*mcp.Tool
	svc   *service.Service
	log   *slog.Logger
}

// New builds the MCP server with every tool, the manifest resource and the
// start_day / close_day prompts.
func New(svc *service.Service, opts Options) *mcp.Server {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	a := &adapter{svc: svc, log: log}
	s := mcp.NewServer(&mcp.Implementation{Name: "stkr", Version: opts.Version}, &mcp.ServerOptions{
		Instructions: "STKR tracks time per context, ideas, follow-up items and coding agents. " +
			"Read the " + ManifestURI + " resource for concepts, tools and privacy rules.",
		Logger: log,
	})
	a.addTools(s)
	a.addResources(s)
	return s
}

// Run serves one stdio session until the client disconnects or ctx ends.
func Run(ctx context.Context, svc *service.Service, opts Options) error {
	return New(svc, opts).Run(ctx, &mcp.StdioTransport{})
}

// toolError maps an error to the text of an MCP tool error: "[code] message".
// Domain errors keep their message; anything else is logged and hidden.
func (a *adapter) toolError(tool string, err error) error {
	code := domain.Code(err)
	if code == "" {
		if errors.Is(err, context.Canceled) {
			return errors.New("[canceled] request canceled")
		}
		a.log.Error("mcp tool failed", "tool", tool, "err", err)
		return errors.New("[internal] internal error; see the stkr log")
	}
	return fmt.Errorf("[%s] %s", code, err.Error())
}

func invalid(format string, args ...any) error {
	return domain.New(domain.ErrInvalid, fmt.Sprintf(format, args...))
}

func checkText(field, s string, max int) error {
	if len(s) > max {
		return invalid("%s is longer than %d bytes", field, max)
	}
	if !utf8.ValidString(s) {
		return invalid("%s is not valid UTF-8", field)
	}
	return nil
}

func checkLimit(n int) (int, error) {
	switch {
	case n < 0:
		return 0, invalid("limit must be positive")
	case n == 0:
		return DefaultLimit, nil
	case n > MaxLimit:
		return 0, invalid("limit must be at most %d", MaxLimit)
	}
	return n, nil
}

func text(s string) []mcp.Content { return []mcp.Content{&mcp.TextContent{Text: s}} }

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
