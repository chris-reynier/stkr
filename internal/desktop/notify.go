package desktop

import "fmt"

// Notice is a tray notification.
type Notice struct {
	Title string
	Text  string
	Error bool
}

// Watcher turns status changes into notifications. It only reports
// transitions: the close becoming due, a new update version becoming
// available and the number of waiting agents going up. The same update
// version is announced once per run.
type Watcher struct {
	closeDue     bool
	agents       int
	announcedVer string
}

// Observe records st and returns the notifications for what changed since
// the previous call. The first call compares against an empty state, so a
// close that is already due is announced once at start.
func (w *Watcher) Observe(st Status) []Notice {
	var out []Notice
	if st.CloseDue && !w.closeDue {
		out = append(out, Notice{Title: "Close the day", Text: "Your close-of-day is due. Use Close day in the STKR menu."})
	}
	if u := st.Update; u != nil && u.Available && u.Version != "" && u.Version != w.announcedVer {
		out = append(out, Notice{Title: "Update available", Text: fmt.Sprintf("STKR %s is available. Choose Install update in the STKR menu.", u.Version)})
		w.announcedVer = u.Version
	}
	if st.AgentsWaiting > w.agents {
		text := "An agent is waiting for your approval."
		if st.AgentsWaiting > 1 {
			text = fmt.Sprintf("%d agents are waiting for your approval.", st.AgentsWaiting)
		}
		out = append(out, Notice{Title: "Agent waiting", Text: text})
	}
	w.closeDue = st.CloseDue
	w.agents = st.AgentsWaiting
	return out
}
