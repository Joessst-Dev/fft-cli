package tui

// Catalog describes every operation the UI can send, and the fft command that
// sends each one. It is read-only, and safe to read from any goroutine.
type Catalog interface {
	// Groups is every operation, grouped by the tag the API documents it under,
	// in tag order. Each operation appears once, under its first tag.
	Groups() []OperationGroup

	// Table renders stdout — what cmd printed under -o json — as the table the same
	// command prints in a shell. It is only asked for a command whose Table is set;
	// "" means there are no rows.
	Table(cmd Command, stdout []byte) (string, error)
}

// OperationGroup is the operations under one tag.
type OperationGroup struct {
	Tag        string
	Operations []Operation
}

// Operation is one operation of the API, as the UI lists and describes it.
type Operation struct {
	ID          string
	Summary     string
	Description string
	Method      string
	Path        string
	Tag         string

	// Permissions are the permissions the API documents for it, any one of which
	// is enough. Empty when it documents none.
	Permissions []string

	// Mutates says the operation changes data on the tenant: fft refuses it on a
	// read-only project, and the UI asks before sending it.
	Mutates bool

	Deprecated bool

	// SampleBody is the request body synthesized from the API's schema, "" when the
	// operation takes none.
	SampleBody string

	// Command is the command that sends it.
	Command Command
}

// Command is an fft command, as the request form needs to know it.
type Command struct {
	// Path is the command line that reaches it, without the program name and
	// before any of the user's arguments: {"facility", "list"}.
	Path []string

	// Curated says the command is hand-written rather than generated from the
	// API's schema.
	Curated bool

	// Args are its positional arguments, in order.
	Args []Arg

	// Flags are the flags the user may fill in. Those the UI decides — the body
	// flags, the global ones — are not among them.
	Flags []Flag

	// Body says the command reads a request body from --file. BodyRequired says it
	// refuses to run without one.
	Body         bool
	BodyRequired bool

	// Example says the command prints a sample body of its own with --example, one
	// that may differ from the operation's SampleBody.
	Example bool

	// Confirms says the command asks before it acts, in its own words and after
	// looking up what it is about to change. The UI sends such a command without
	// asking first, and puts the command's question to the user instead.
	Confirms bool

	// Table says [Catalog.Table] can render what the command prints.
	Table bool
}

// Arg is a positional argument.
type Arg struct {
	Name     string
	Required bool
}

// FlagKind is the kind of value a flag takes.
type FlagKind int

// The kinds of value a flag takes. The form checks a value against its kind
// before anything runs; the command checks it again.
const (
	// FlagString takes any text.
	FlagString FlagKind = iota
	// FlagBool is on or off, and takes no value.
	FlagBool
	// FlagInt takes a whole number.
	FlagInt
	// FlagFloat takes a number.
	FlagFloat
	// FlagDuration takes a Go duration: 30s, 2m.
	FlagDuration
	// FlagList takes the flag once per value.
	FlagList
)

// Flag is one flag the user may give.
type Flag struct {
	Name     string
	Kind     FlagKind
	Usage    string
	Required bool

	// Default is the value the command uses when the flag is not given, as the
	// flag spells it; "" when it has none worth showing.
	Default string

	// Enum lists the values the flag accepts, empty when it accepts any.
	Enum []string

	// WithExample says the flag may be given together with --example, and so may
	// shape the sample body the command prints.
	WithExample bool
}
