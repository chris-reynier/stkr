package desktop

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Action ids used by menu items. Ids with a ":" carry an argument.
const (
	ActStop          = "stop"
	ActBookmarkClip  = "bookmark-clipboard"
	ActStartDay      = "start-day"
	ActCloseDay      = "close-day"
	ActImportClip    = "import-clipboard"
	ActDataFolder    = "folder:data"
	ActLogsFolder    = "folder:logs"
	ActAbout         = "about"
	ActQuit          = "quit"
	ActOpenPanel     = "open-panel"
	prefixSwitch     = "switch:"
	prefixOpen       = "open:"
	pageToday        = "/ui/today"
	pageIdeas        = "/ui/ideas"
	pageSettings     = "/ui/settings"
	maxMenuLabelRune = 60
)

// MenuItem is one entry of the tray menu. An item with Children is a
// submenu; Separator items have no label.
type MenuItem struct {
	ID        string
	Label     string
	Disabled  bool
	Checked   bool
	Separator bool
	Children  []MenuItem
}

// openPages are the entries of the Open submenu, in nav order.
var openPages = []struct{ label, path string }{
	{"Today", pageToday}, {"Week", "/ui/week"}, {"Agents", "/ui/agents"},
	{"Ideas", pageIdeas}, {"Links", "/ui/links"}, {"Settings", pageSettings},
}

// BuildMenu builds the tray menu from the last status. st is nil when the
// server could not be reached; the actions that need it are then disabled.
func BuildMenu(st *Status, version string) []MenuItem {
	sep := MenuItem{Separator: true}
	offline := st == nil
	header := "Nothing running"
	switch {
	case offline:
		header = "STKR server not reachable"
	case st.Now != nil:
		header = fmt.Sprintf("%s - %s", shorten(labelOf(st.Now.Label, st.Now.ContextID), maxMenuLabelRune), FormatElapsed(st.Now.ElapsedS))
		if st.Now.IdleSuspect {
			header += " (idle?)"
		}
	}
	items := []MenuItem{{Label: header, Disabled: true}, sep}

	var switchTo []MenuItem
	if !offline {
		for _, c := range st.Contexts {
			switchTo = append(switchTo, MenuItem{
				ID:      prefixSwitch + c.ID,
				Label:   shorten(labelOf(c.Label, c.ID), maxMenuLabelRune),
				Checked: st.Now != nil && st.Now.ContextID == c.ID,
			})
		}
	}
	if len(switchTo) > 0 {
		switchTo = append(switchTo, sep)
	}
	switchTo = append(switchTo, MenuItem{ID: prefixOpen + pageSettings, Label: "Manage projects..."})
	items = append(items,
		MenuItem{Label: "Switch to", Children: switchTo, Disabled: offline},
		MenuItem{ID: ActStop, Label: "Stop", Disabled: offline || st.Now == nil},
		sep,
		MenuItem{ID: prefixOpen + pageIdeas, Label: "Capture idea...", Disabled: offline},
		MenuItem{ID: ActBookmarkClip, Label: "Save clipboard as bookmark", Disabled: offline},
		sep,
		MenuItem{ID: ActStartDay, Label: "Start day", Disabled: offline},
		MenuItem{ID: ActCloseDay, Label: closeLabel(st), Disabled: offline},
		MenuItem{ID: ActImportClip, Label: "Import answer from clipboard", Disabled: offline},
		sep,
	)
	var open []MenuItem
	for _, p := range openPages {
		label := p.label
		if p.path == pageIdeas && !offline && st.IdeasNew > 0 {
			label = fmt.Sprintf("Ideas (%d new)", st.IdeasNew)
		}
		if p.path == "/ui/agents" && !offline && st.AgentsWaiting > 0 {
			label = fmt.Sprintf("Agents (%d waiting)", st.AgentsWaiting)
		}
		open = append(open, MenuItem{ID: prefixOpen + p.path, Label: label})
	}
	items = append(items,
		MenuItem{Label: "Open", Children: open, Disabled: offline},
		MenuItem{ID: ActDataFolder, Label: "Data folder"},
		MenuItem{ID: ActLogsFolder, Label: "Logs folder"},
		sep,
		MenuItem{ID: ActAbout, Label: "About STKR " + version},
		MenuItem{ID: ActQuit, Label: "Quit"},
	)
	return items
}

func closeLabel(st *Status) string {
	if st != nil && st.CloseDue {
		return "Close day (due)"
	}
	return "Close day"
}

// RenderMenu writes the menu as indented text (used by golden tests).
func RenderMenu(items []MenuItem) string {
	var b strings.Builder
	var walk func([]MenuItem, int)
	walk = func(list []MenuItem, depth int) {
		for _, it := range list {
			b.WriteString(strings.Repeat("  ", depth))
			if it.Separator {
				b.WriteString("----\n")
				continue
			}
			if it.Checked {
				b.WriteString("[x] ")
			}
			b.WriteString(it.Label)
			if len(it.Children) > 0 {
				b.WriteString(" >")
			}
			if it.ID != "" {
				b.WriteString("  {" + it.ID + "}")
			}
			if it.Disabled {
				b.WriteString("  (disabled)")
			}
			b.WriteString("\n")
			walk(it.Children, depth+1)
		}
	}
	walk(items, 0)
	return b.String()
}

// FormatElapsed formats seconds as "1h12" (hours and two-digit minutes).
func FormatElapsed(s int64) string {
	if s < 0 {
		s = 0
	}
	m := s / 60
	return fmt.Sprintf("%dh%02d", m/60, m%60)
}

// Tooltip is the icon tooltip: "STKR - <label> 1h12", at most 127 characters.
func Tooltip(st *Status) string {
	t := "STKR - Nothing running"
	switch {
	case st == nil:
		t = "STKR - server not reachable"
	case st.Now != nil:
		t = fmt.Sprintf("STKR - %s %s", labelOf(st.Now.Label, st.Now.ContextID), FormatElapsed(st.Now.ElapsedS))
		if st.Now.IdleSuspect {
			t += " (idle?)"
		}
	}
	if st != nil && st.CloseDue {
		t += " - close due"
	}
	return shorten(t, 127)
}

func labelOf(label, id string) string {
	if label = strings.TrimSpace(label); label != "" {
		return label
	}
	return id
}

// shorten cuts s to at most n runes, ending with "..." when cut. Menu
// separators and accelerators ("&") are escaped by the Win32 layer.
func shorten(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-3]) + "..."
}
