package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

// formField is one row of the add form.
type formField struct {
	label string
	kind  fieldKind
	input textinput.Model
	on    bool
}

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldSecret
	fieldToggle
	fieldCheck
)

// The add form's rows, in order. They are the flags of `fft project add`.
const (
	rowName = iota
	rowBaseURL
	rowAPIKey
	rowSignInWith
	rowLogin
	rowProjectID
	rowEnvironment
	rowTenant
	rowPassword
	rowReadOnly
	rowForce
	rowCount
)

type formKeys struct {
	next   key.Binding
	prev   key.Binding
	toggle key.Binding
	submit key.Binding
	cancel key.Binding
}

// formEvent is what a key did to the form as a whole.
type formEvent int

const (
	formEditing formEvent = iota
	formSubmitted
	formCancelled
)

// addForm collects a new project. Its secrets — the API key and the password —
// are masked on screen and leave the form only as the run's stdin.
type addForm struct {
	st     styles
	fields []*formField
	focus  int
	keys   formKeys

	// problems are what the form itself found wrong, before anything ran.
	problems []string

	// failure is what `project add` said when it refused.
	failure *failure

	// submitting is set while the add runs, so that a second enter does not send a
	// second one.
	submitting bool
}

func newAddForm(st styles) *addForm {
	f := &addForm{
		st: st,
		keys: formKeys{
			next:   key.NewBinding(key.WithKeys("tab", "down", "enter"), key.WithHelp("tab/enter", "next field")),
			prev:   key.NewBinding(key.WithKeys("shift+tab", "up"), key.WithHelp("shift+tab", "previous field")),
			toggle: key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle")),
			submit: key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "add project")),
			cancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		},
	}

	f.fields = make([]*formField, rowCount)
	text := func(label, placeholder string, kind fieldKind) *formField {
		in := textinput.New()
		in.Prompt = ""
		in.Placeholder = placeholder
		in.SetStyles(st.input)
		if kind == fieldSecret {
			in.EchoMode = textinput.EchoPassword
			in.EchoCharacter = '•'
		}
		return &formField{label: label, kind: kind, input: in}
	}

	f.fields[rowName] = text("Name", "staging", fieldText)
	f.fields[rowBaseURL] = text("Base URL", "https://acme.api.fulfillmenttools.com", fieldText)
	f.fields[rowAPIKey] = text("API key", "the Firebase Web API key", fieldSecret)
	f.fields[rowSignInWith] = &formField{label: "Sign in with", kind: fieldToggle}
	f.fields[rowLogin] = text("Username", "the short login name", fieldText)
	f.fields[rowProjectID] = text("Project id", "acme", fieldText)
	f.fields[rowEnvironment] = text("Environment", "pre or prd", fieldText)
	f.fields[rowTenant] = text("Tenant", "optional", fieldText)
	f.fields[rowPassword] = text("Password", "", fieldSecret)
	f.fields[rowReadOnly] = &formField{label: "Read-only", kind: fieldCheck}
	f.fields[rowForce] = &formField{label: "Replace existing", kind: fieldCheck}

	f.fields[rowName].focus()
	return f
}

func (f *addForm) useEmail() bool { return f.fields[rowSignInWith].on }

func (f *addForm) value(row int) string {
	return strings.TrimSpace(f.fields[row].input.Value())
}

func (f *addForm) update(msg tea.KeyPressMsg) (formEvent, tea.Cmd) {
	switch {
	case key.Matches(msg, f.keys.cancel):
		return formCancelled, nil
	case key.Matches(msg, f.keys.submit):
		return f.submit()
	case msg.String() == "enter" && f.focus == rowCount-1:
		return f.submit()
	case key.Matches(msg, f.keys.next):
		f.move(1)
		return formEditing, nil
	case key.Matches(msg, f.keys.prev):
		f.move(-1)
		return formEditing, nil
	}

	field := f.fields[f.focus]
	switch field.kind {
	case fieldToggle, fieldCheck:
		if key.Matches(msg, f.keys.toggle) {
			field.on = !field.on
			if f.focus == rowSignInWith {
				f.relabelLogin()
			}
		}
		return formEditing, nil
	default:
		var cmd tea.Cmd
		field.input, cmd = field.input.Update(msg)
		return formEditing, cmd
	}
}

func (f *addForm) submit() (formEvent, tea.Cmd) {
	if f.submitting {
		return formEditing, nil
	}
	f.problems = f.validate()
	if len(f.problems) > 0 {
		return formEditing, nil
	}
	return formSubmitted, nil
}

func (f *addForm) move(by int) {
	f.fields[f.focus].blur()
	f.focus = (f.focus + by + rowCount) % rowCount
	f.fields[f.focus].focus()
}

// focus gives a text row the cursor. A toggle or a checkbox has no text input to
// focus: its zero value was never built, and focusing it would start a cursor that
// does not exist.
func (ff *formField) focus() {
	if ff.kind == fieldText || ff.kind == fieldSecret {
		ff.input.Focus()
	}
}

func (ff *formField) blur() {
	if ff.kind == fieldText || ff.kind == fieldSecret {
		ff.input.Blur()
	}
}

func (f *addForm) relabelLogin() {
	login := f.fields[rowLogin]
	if f.useEmail() {
		login.label = "Email"
		login.input.Placeholder = "the full sign-in address"
	} else {
		login.label = "Username"
		login.input.Placeholder = "the short login name"
	}
}

// validate finds what `project add` would refuse, so the form can say so before
// anything runs. It is a courtesy, not the check: the command validates again.
func (f *addForm) validate() []string {
	var problems []string
	require := func(row int, what string) {
		if f.value(row) == "" {
			problems = append(problems, what+" is required")
		}
	}

	require(rowName, "the name")
	if name := f.value(rowName); name != "" {
		if err := secrets.ValidateProjectName(name); err != nil {
			problems = append(problems, err.Error())
		}
	}

	require(rowBaseURL, "the base URL")
	if u := f.value(rowBaseURL); u != "" && !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		problems = append(problems, "the base URL must start with https://")
	}

	require(rowAPIKey, "the API key")

	login := f.value(rowLogin)
	switch {
	case f.useEmail():
		require(rowLogin, "the email")
		if login != "" && !strings.Contains(login, "@") {
			problems = append(problems, "the email must be a full address")
		}
	default:
		require(rowLogin, "the username")
		if strings.Contains(login, "@") {
			problems = append(problems, "the username is the short login name; switch to email for a full address")
		}
		require(rowProjectID, "the project id (to build the sign-in address)")
		require(rowEnvironment, "the environment (to build the sign-in address)")
	}

	if f.fields[rowPassword].input.Value() == "" {
		problems = append(problems, "the password is required")
	}
	return problems
}

// action is the `project add` the form describes. The secrets travel on stdin —
// the API key on the first line, the password after it — so that neither the
// argument list nor its display can hold them.
func (f *addForm) action() action {
	args := []string{"project", "add", f.value(rowName),
		"--base-url", f.value(rowBaseURL),
	}
	if f.useEmail() {
		args = append(args, "--email", f.value(rowLogin))
	} else {
		args = append(args, "--username", f.value(rowLogin))
	}
	for _, opt := range []struct {
		row  int
		flag string
	}{
		{rowProjectID, "--project-id"},
		{rowEnvironment, "--env"},
		{rowTenant, "--tenant"},
	} {
		if v := f.value(opt.row); v != "" {
			args = append(args, opt.flag, v)
		}
	}
	if f.fields[rowReadOnly].on {
		args = append(args, "--read-only")
	}
	if f.fields[rowForce].on {
		args = append(args, "--force")
	}
	args = append(args, "--api-key-stdin", "--password-stdin")

	// The password is sent exactly as typed; the key is trimmed, as the flag is.
	stdin := f.value(rowAPIKey) + "\n" + f.fields[rowPassword].input.Value()
	return action{
		inv:     Invocation{Args: args, Stdin: []byte(stdin), Exclusive: true},
		display: commandLine(args),
	}
}

func (f *addForm) bindings() []key.Binding {
	return []key.Binding{f.keys.next, f.keys.prev, f.keys.toggle, f.keys.submit, f.keys.cancel}
}

func (f *addForm) view(width int) string {
	st := f.st
	lines := []string{st.title.Render("Add a project"), ""}
	for i, field := range f.fields {
		marker := "  "
		if i == f.focus {
			marker = "> "
		}
		label := fmt.Sprintf("%s%-17s", marker, field.label)
		if i == f.focus {
			label = st.selected.Render(label)
		}

		var val string
		switch field.kind {
		case fieldToggle:
			if field.on {
				val = "( ) username  (•) email"
			} else {
				val = "(•) username  ( ) email"
			}
		case fieldCheck:
			val = "[ ]"
			if field.on {
				val = "[x]"
			}
		default:
			val = field.input.View()
		}
		lines = append(lines, clip(label+val, width))
	}

	lines = append(lines, "")
	if f.submitting {
		lines = append(lines, st.dim.Render("Adding the project and checking its credentials…"))
	}
	for _, p := range f.problems {
		lines = append(lines, st.errorText.Render("• "+p))
	}
	if f.failure != nil {
		lines = append(lines, f.failure.view(st, width))
	}
	lines = append(lines, st.dim.Render("The API key and password are passed on stdin, never on the command line."))
	return strings.Join(lines, "\n")
}
