package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// whoami is `fft auth whoami -o json`.
type whoami struct {
	UserID string      `json:"userId"`
	Email  string      `json:"email"`
	Roles  []roleGrant `json:"roles"`
}

// scope is what a read about the current project is about: the project, and the
// selection that chose it. A read for another scope says nothing about this one.
type scope struct {
	switches uint64
	project  string
}

func (s *session) scope() scope {
	return scope{switches: s.switches, project: s.currentProject()}
}

type roleKeys struct {
	up      key.Binding
	down    key.Binding
	refresh key.Binding
}

func newRoleKeys() roleKeys {
	return roleKeys{
		up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "scroll")),
		down:    key.NewBinding(key.WithKeys("down", "j")),
		refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	}
}

// rolesScreen shows the user's roles on the current project, what they permit,
// and the operations they appear not to permit. What it reads is also what the
// Operations list greys, and what a question about sending a request warns of.
type rolesScreen struct {
	s    *session
	st   styles
	keys roleKeys

	// ops is every operation, in the catalog's order.
	ops []Operation

	// asked is the scope roles were last asked for, and requested whether they
	// have been asked for at all. gen counts the reads, so that only the last
	// one's answer is taken.
	asked     scope
	requested bool
	gen       uint64
	loading   bool

	// shown is the scope what is on display was read for, and project the project
	// whoami answered for; me and missing are nil until a read has succeeded.
	shown   scope
	project string
	me      *whoami
	missing []Operation
	failure *failure
	notice  string

	scroll int
}

func newRolesScreen(s *session, st styles, cat Catalog) *rolesScreen {
	r := &rolesScreen{s: s, st: st, keys: newRoleKeys()}
	if cat != nil {
		for _, g := range cat.Groups() {
			r.ops = append(r.ops, g.Operations...)
		}
	}
	return r
}

func (r *rolesScreen) focused() bool { return false }

// want reads the roles, unless they have been asked for the current project
// already. It is called while a screen that uses them is on display, so the
// request is only made once somebody looks.
func (r *rolesScreen) want() tea.Cmd {
	if r.requested && r.asked == r.s.scope() {
		return nil
	}
	return r.load()
}

func (r *rolesScreen) load() tea.Cmd {
	asked := r.s.scope()
	r.asked, r.requested, r.loading = asked, true, true
	r.gen++
	gen := r.gen

	a := r.s.scoped("auth", "whoami")
	target := r.s.target()
	a.inv.Project = target
	return r.s.start(a, func(res Result) tea.Cmd {
		// An answer for a project the UI has left is not about this one, and the
		// switch asks again once the roles are looked at.
		if gen != r.gen || r.s.switches != asked.switches {
			return nil
		}
		r.loading = false
		r.shown = asked
		r.me, r.missing, r.failure, r.notice = nil, nil, nil, ""
		r.s.grants = nil

		switch res.ExitCode {
		case exitcode.OK:
		case exitcode.Config:
			r.notice = "No project is selected, so there are no roles to show. Add or choose one on the Projects screen (1)."
			return nil
		default:
			r.failure = &failure{what: "reading your roles", result: res}
			return nil
		}

		var me whoami
		if err := json.Unmarshal(res.Stdout, &me); err != nil {
			r.failure = &failure{
				what:   "reading your roles",
				result: Result{ExitCode: exitcode.General, Stderr: []byte(err.Error())},
			}
			return nil
		}
		r.me = &me
		r.project = firstNonEmpty(res.Project, target, asked.project)
		r.s.grants = newGrants(r.project, me.Roles)
		for _, op := range r.ops {
			if r.s.grants.check(op) == accessLacking {
				r.missing = append(r.missing, op)
			}
		}
		return nil
	})
}

func (r *rolesScreen) update(msg tea.Msg) tea.Cmd {
	keyMsg, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		return nil
	}
	switch {
	case key.Matches(keyMsg, r.keys.refresh):
		return r.load()
	case key.Matches(keyMsg, r.keys.up):
		r.scroll = max(r.scroll-1, 0)
	case key.Matches(keyMsg, r.keys.down):
		r.scroll++
	}
	return nil
}

func (r *rolesScreen) bindings() []key.Binding {
	return []key.Binding{r.keys.up, r.keys.refresh}
}

func (r *rolesScreen) equivalent() shellCommand {
	return r.s.scoped("auth", "whoami").display
}

func (r *rolesScreen) view(width, height int) string {
	st := r.st
	head := []string{st.title.Render("Roles"), ""}
	current := r.shown == r.s.scope()

	switch {
	case r.loading && (!current || r.me == nil):
		head = append(head, st.dim.Render("Reading your roles…"))
		return strings.Join(head, "\n")
	case !r.requested || !current:
		head = append(head, st.dim.Render("Your roles are read when you look at them. Press r to read them now."))
		return strings.Join(head, "\n")
	case r.notice != "":
		head = append(head, wrap(r.notice, width))
		return strings.Join(head, "\n")
	case r.failure != nil:
		head = append(head,
			r.failure.view(st, width),
			"",
			wrap(st.warnText.Render("Your roles are unknown, so no operation is greyed. Press r to try again."), width))
		return strings.Join(head, "\n")
	}

	me := r.me
	project := r.project
	if r.s.headless {
		project += " (environment)"
	}
	head = append(head, wrap(output.SanitizeCell(fmt.Sprintf("Signed in as %s (user %s) on %s.",
		me.Email, me.UserID, project)), width), "")

	body := r.roleLines(width)
	body = append(body, "")
	body = append(body, r.missingLines(width)...)
	body = append(body, "")
	body = append(body, strings.Split(wrap(st.dim.Render("Greying is only a hint: a role can be limited to some "+
		"facilities, which fft cannot see. A greyed operation can still be sent, and the tenant decides."), width), "\n")...)

	top := strings.Join(head, "\n")
	room := max(height-lipgloss.Height(top), 1)
	r.scroll = min(r.scroll, max(len(body)-room, 0))
	shown := body[r.scroll:min(r.scroll+room, len(body))]
	return strings.Join(append([]string{top}, shown...), "\n")
}

// roleLines is the role → permissions table, a role's permissions wrapped under
// its column.
func (r *rolesScreen) roleLines(width int) []string {
	st := r.st
	if len(r.me.Roles) == 0 {
		return strings.Split(wrap("You hold no roles on this project, so the tenant refuses every request "+
			"that needs a permission.", width), "\n")
	}
	nameWidth := len("ROLE")
	for _, role := range r.me.Roles {
		nameWidth = max(nameWidth, ansi.StringWidth(output.SanitizeCell(role.Name)))
	}
	gap := strings.Repeat(" ", nameWidth+2)
	lines := []string{st.dim.Render(pad("ROLE", nameWidth) + "  PERMISSIONS")}
	for _, role := range r.me.Roles {
		perms := "none"
		if len(role.Permissions) > 0 {
			perms = output.SanitizeCell(strings.Join(role.Permissions, ", "))
		}
		wrapped := strings.Split(ansi.Wrap(perms, max(width-nameWidth-2, 20), ""), "\n")
		lines = append(lines, pad(output.SanitizeCell(role.Name), nameWidth)+"  "+wrapped[0])
		for _, more := range wrapped[1:] {
			lines = append(lines, gap+more)
		}
	}
	return lines
}

// missingLines lists the operations the roles appear not to permit.
func (r *rolesScreen) missingLines(width int) []string {
	st := r.st
	if len(r.missing) == 0 {
		return []string{"Your roles permit every operation whose permission the API documents."}
	}
	noun := "operations"
	if len(r.missing) == 1 {
		noun = "operation"
	}
	lines := []string{st.title.Render(fmt.Sprintf("You appear to lack the permission for %d %s", len(r.missing), noun))}
	for _, op := range r.missing {
		line := "  " + pad(output.SanitizeCell(op.ID), opIDWidth) + " " +
			output.SanitizeCell(op.Method+" "+op.Path) + "  " +
			st.dim.Render("needs "+needs(op.Permissions))
		lines = append(lines, clip(line, width))
	}
	return lines
}

// needs names the permissions an operation accepts, any one of which is enough.
func needs(perms []string) string {
	if len(perms) == 1 {
		return output.SanitizeCell(perms[0])
	}
	return "any of " + output.SanitizeCell(strings.Join(perms, ", "))
}
