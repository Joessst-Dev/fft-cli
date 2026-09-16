package history

import (
	"cmp"
	"slices"
	"time"
)

// Usage is how often one operation was used in one project.
type Usage struct {
	Project     string `json:"project"`
	OperationID string `json:"operationId"`

	// Command is the command path the operation was last reached through.
	Command string `json:"command"`

	Count    int       `json:"count"`
	LastUsed time.Time `json:"lastUsed"`
}

// Top returns the n most used operations in entries, most used first and the
// more recently used first among equals. project narrows it to one project, and
// "" counts every project, each separately. n <= 0 returns them all.
func Top(entries []Entry, project string, n int) []Usage {
	type key struct{ project, operation string }

	byKey := make(map[key]*Usage)
	for _, e := range entries {
		if project != "" && e.Project != project {
			continue
		}
		k := key{e.Project, e.OperationID}
		u, ok := byKey[k]
		if !ok {
			u = &Usage{Project: e.Project, OperationID: e.OperationID}
			byKey[k] = u
		}
		u.Count++
		if !e.TS.Before(u.LastUsed) {
			u.LastUsed = e.TS
			u.Command = e.Command
		}
	}

	out := make([]Usage, 0, len(byKey))
	for _, u := range byKey {
		out = append(out, *u)
	}
	slices.SortFunc(out, func(a, b Usage) int {
		return cmp.Or(
			cmp.Compare(b.Count, a.Count),
			b.LastUsed.Compare(a.LastUsed),
			cmp.Compare(a.Project, b.Project),
			cmp.Compare(a.OperationID, b.OperationID),
		)
	})

	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}
