package ui

import (
	"strings"

	"github.com/egoist/mygo/internal/platform"
)

// liveState is what the live regions of the last tree held, which the
// next tree's are told from: the nodes in logs (the logs among them), and
// the texts alerts showed, and the text of each status. spare holds the
// maps of the tree before, reused.
type liveState struct {
	seen, spareSeen         map[uint64]bool
	alerts, spareAlerts     map[string]bool
	statuses, spareStatuses map[uint64]string
}

// liveRegions adds to a frame's tree what its live regions read out, as
// ARIA's do on the web: the content added to a log (RoleLog), politely,
// after what is being read, and an alert's (RoleAlert) as it appears or
// changes, at once. Screen readers read neither from the platforms' own
// live regions outside web content, but all read announcements. Alerts
// are told by their text, which an element's ID shifting as others
// appear before it keeps. A status (RoleStatus) is read as ARIA's is,
// politely and whole (aria-atomic), when its text changes: all the text
// inside it, after what is being read, as a log's additions are.
// A status is told from the last tree's by its element's ID, which
// keyed elements (Key) and elements whose siblings before them stay keep
// from frame to frame, as a log's rows are; one whose ID shifted is read
// as one appearing, which reads nothing, rather than as another's change.
// A status inside a log, an alert or another status is read as part of
// it, not on its own. Busy is not honoured, as for logs. speak is false for the first tree assistive
// technology gets, whose content was there before it came.
func (rt *engine) liveRegions(t *platform.AccessTree, speak bool) {
	l := &rt.live
	has := false
	for i := range t.Nodes {
		if r := t.Nodes[i].Role; r == platform.RoleLog || r == platform.RoleAlert || r == platform.RoleStatus {
			has = true
			break
		}
	}
	if !has {
		// As on most frames: nothing to tell, nor to keep.
		clear(l.seen)
		clear(l.alerts)
		clear(l.statuses)
		return
	}
	if l.spareSeen == nil {
		l.spareSeen, l.spareAlerts = map[uint64]bool{}, map[string]bool{}
		l.spareStatuses = map[uint64]string{}
	}
	if l.statuses == nil {
		l.statuses = map[uint64]string{}
	}
	seen, alerts, statuses := l.spareSeen, l.spareAlerts, l.spareStatuses
	// The log each node is in, its own if it is one: the nearest.
	logOf := make([]int, len(t.Nodes))
	added := map[int]string{}
	var logs []int
	for i, n := range t.Nodes {
		logOf[i] = -1
		switch {
		case n.Role == platform.RoleLog:
			logOf[i] = i
		case n.Role == platform.RoleAlert:
			// What is inside it is read as the alert's.
		case n.Parent >= 0:
			logOf[i] = logOf[n.Parent]
		}
		r := logOf[i]
		if r < 0 {
			continue
		}
		seen[n.ID] = true
		// What a log that showed in the last frame gained, at the top of
		// what is new: what is inside it is read with it.
		if r == i || !speak || !l.seen[t.Nodes[r].ID] || l.seen[n.ID] || n.Parent != r && !l.seen[t.Nodes[n.Parent].ID] {
			continue
		}
		if text := liveText(t, i); text != "" {
			if added[r] == "" {
				logs = append(logs, r)
				added[r] = text
			} else {
				added[r] += " " + text
			}
		}
	}
	for _, r := range logs {
		t.Announcements = append(t.Announcements, added[r])
	}
	for i, n := range t.Nodes {
		if n.Role != platform.RoleAlert {
			continue
		}
		text := liveText(t, i)
		if text == "" {
			text = n.Label
		}
		if text == "" {
			continue
		}
		if speak && !l.alerts[text] && !alerts[text] {
			t.Alerts = append(t.Alerts, text)
		}
		alerts[text] = true
	}
	// Statuses outside the other live regions and statuses (their nodes
	// follow their parents').
	inside := make([]bool, len(t.Nodes))
	for i, n := range t.Nodes {
		if p := n.Parent; p >= 0 {
			switch t.Nodes[p].Role {
			case platform.RoleLog, platform.RoleAlert, platform.RoleStatus:
				inside[i] = true
			default:
				inside[i] = inside[p]
			}
		}
		if n.Role != platform.RoleStatus || inside[i] {
			continue
		}
		text := liveText(t, i)
		if text == "" {
			text = n.Label
		}
		if before, shown := l.statuses[n.ID]; speak && shown && text != "" && text != before {
			t.Announcements = append(t.Announcements, text)
		}
		statuses[n.ID] = text
	}
	clear(l.seen)
	clear(l.alerts)
	clear(l.statuses)
	l.seen, l.spareSeen = seen, l.seen
	l.alerts, l.spareAlerts = alerts, l.alerts
	l.statuses, l.spareStatuses = statuses, l.statuses
}

// liveText returns the text of node i and those inside it, as a live
// region reads it: the names of the nodes without children, a text's
// whole, joined by spaces.
func liveText(t *platform.AccessTree, i int) string {
	var b strings.Builder
	add := func(s string) {
		if s == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(s)
	}
	// The nodes inside node i follow it, their parents at i or after.
	end := i + 1
	for end < len(t.Nodes) && t.Nodes[end].Parent >= i {
		end++
	}
	for j := i; j < end; j++ {
		n := t.Nodes[j]
		leaf := j+1 == end || t.Nodes[j+1].Parent != j
		if n.Role == platform.RoleText {
			add(n.Label)
			// Its runs, as links, are in its text.
			for k := j; j+1 < end && t.Nodes[j+1].Parent >= k; {
				j++
			}
			continue
		}
		if leaf {
			add(n.Label)
		}
	}
	return b.String()
}
