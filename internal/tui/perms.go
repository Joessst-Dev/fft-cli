package tui

import (
	"strings"

	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// access is what the UI can tell about whether the user may call an operation.
type access int

const (
	// accessUnknown is the answer whenever the UI cannot tell: the user's roles are
	// not known — not read yet, or whoami failed — or the API documents no
	// permission for the operation. Nothing is greyed for it.
	accessUnknown access = iota
	// accessGranted means one of the roles holds one of the operation's permissions.
	accessGranted
	// accessLacking means none of the roles holds any of them.
	accessLacking
)

// grants is what the user's roles permit on one project, as `fft auth whoami`
// reported it.
//
// It is a hint and never a gate. A role can be limited to some facilities, which
// whoami does not say and the UI could not check against a request anyway, so an
// operation the user appears to lack is still offered, and still sent when asked.
type grants struct {
	// project is the project whoami answered for.
	project string

	roles []roleGrant

	// held is every permission any role carries.
	held map[string]bool
}

// roleGrant is one role and the permissions it carries.
type roleGrant struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

func newGrants(project string, roles []roleGrant) *grants {
	g := &grants{project: project, roles: roles, held: make(map[string]bool)}
	for _, r := range roles {
		for _, p := range r.Permissions {
			g.held[p] = true
		}
	}
	return g
}

// check says whether the user may call op. The API lists the permissions an
// operation accepts, and any one of them is enough; whoami names permissions in
// the same words.
func (g *grants) check(op Operation) access {
	if g == nil || len(op.Permissions) == 0 {
		return accessUnknown
	}
	for _, p := range op.Permissions {
		if g.held[p] {
			return accessGranted
		}
	}
	return accessLacking
}

// lacking is the permissions op wants that the user appears to hold none of, nil
// unless check says [accessLacking].
func (g *grants) lacking(op Operation) []string {
	if g.check(op) != accessLacking {
		return nil
	}
	return op.Permissions
}

// lackingNote is what a question about sending op says when the user appears to
// lack its permission, "" when they do not, or when nobody can tell.
func lackingNote(missing []string) string {
	if len(missing) == 0 {
		return ""
	}
	what := missing[0]
	if len(missing) > 1 {
		what = "any of " + strings.Join(missing, ", ")
	}
	return "You appear to lack " + output.SanitizeCell(what) +
		", so the tenant may refuse this with 403. Your roles may still allow it for some facilities."
}
