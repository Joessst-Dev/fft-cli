package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// The terminal answers the background query (OSC 11) on the same stream as the
// keys. Bubble Tea's reader waits only a few milliseconds for the rest of an
// escape sequence, so a reply that a slow link (ssh, tmux) splits in two arrives
// as an unknown event followed by key presses: hex digits, r, g, b, / and the
// terminator. Each of those is a key the screens bind, so the UI must not act on
// them.

// backgroundWait is how long after asking the UI takes no keys but ctrl+c, unless
// the answer comes first. Nobody types in the first moments of a new screen.
const backgroundWait = 250 * time.Millisecond

// replyWait and replyMaxKeys bound how much of a split reply's tail is dropped:
// a reply has about twenty characters, and they follow the head closely.
const (
	replyWait    = time.Second
	replyMaxKeys = 32
)

// oscBackground is how a reply to the background query starts.
const oscBackground = "\x1b]11;"

// replyEnd is how the terminal ends the reply, as the reader decodes each
// terminator on its own: ESC \ (ST), BEL, the backslash of an ST whose ESC came
// alone, and the 8-bit ST.
var replyEnd = key.NewBinding(key.WithKeys("alt+\\", "ctrl+g", "\\", "ctrl+alt+\\"))

// replyStart is ESC ] decoded on its own, the head of a reply split right after it.
var replyStart = key.NewBinding(key.WithKeys("alt+]"))

// backgroundQuery tracks the question the UI asks the terminal about its
// background, so that the answer is never read as keys.
type backgroundQuery struct {
	// asked is when the question was sent, and pending holds until an answer, or
	// the head of one, has come.
	asked   time.Time
	pending bool

	// Once the head of a split reply has come, until is when dropping its tail
	// stops, and left is how many more keys may be dropped.
	until time.Time
	left  int
}

// ask notes that the question was sent at now.
func (q *backgroundQuery) ask(now time.Time) {
	q.asked, q.pending = now, true
}

// answered notes that the whole reply came.
func (q *backgroundQuery) answered() {
	q.pending, q.left = false, 0
}

// unknown reads an event the terminal reader could not decode, and reports
// whether it was the head of a split reply.
func (q *backgroundQuery) unknown(ev uv.UnknownEvent, now time.Time) bool {
	head := string(ev)
	isReply := strings.HasPrefix(head, oscBackground) ||
		(strings.HasPrefix(head, "\x1b]") && strings.HasPrefix(oscBackground, head))
	if !q.pending || !isReply {
		return false
	}
	q.startTail(now)
	return true
}

func (q *backgroundQuery) startTail(now time.Time) {
	q.pending = false
	q.until, q.left = now.Add(replyWait), replyMaxKeys
}

// swallow reports whether msg is part of the reply rather than a key the user
// pressed.
func (q *backgroundQuery) swallow(msg tea.KeyPressMsg, now time.Time) bool {
	if q.pending && key.Matches(msg, replyStart) {
		q.startTail(now)
		return true
	}
	if q.left > 0 {
		if now.After(q.until) || !inReply(msg) {
			// Not the reply after all: the key is the user's.
			q.left = 0
			return false
		}
		q.left--
		if key.Matches(msg, replyEnd) {
			q.left = 0
		}
		return true
	}
	return q.pending && now.Sub(q.asked) < backgroundWait
}

// inReply reports whether msg can be a character of a reply's tail: the colour
// (rgb:1e1e/1e1e/1e1e, or #1e1e1e), the rest of the introducer, or the
// terminator.
func inReply(msg tea.KeyPressMsg) bool {
	if key.Matches(msg, replyEnd) || msg.Code == tea.KeyEscape {
		return true
	}
	if len(msg.Text) != 1 {
		return false
	}
	return strings.ContainsAny(msg.Text, "0123456789abcdefABCDEFrgb:/#;]")
}
