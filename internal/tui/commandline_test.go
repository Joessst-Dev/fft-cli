package tui

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("commandLine quotes what a shell would misread",
	func(args []string, want string) {
		cmd := commandLine(args)
		Expect(cmd.line).To(Equal(want))
		Expect(cmd.unportable).To(BeFalse())
		Expect(cmd.String()).To(Equal(want))
	},
	Entry("plain words", []string{"project", "use", "staging"}, "fft project use staging"),
	Entry("a URL", []string{"--base-url", "https://a.example.com/x"}, "fft --base-url https://a.example.com/x"),
	Entry("a space", []string{"use", "my project"}, "fft use 'my project'"),
	Entry("an empty value", []string{"--tenant", ""}, "fft --tenant ''"),
	Entry("a dollar", []string{"use", "$HOME"}, "fft use '$HOME'"),
	Entry("a count", []string{"--size", "25"}, "fft --size 25"),
	Entry("PowerShell's array operator", []string{"--status", "OPEN,CLOSED"}, "fft --status 'OPEN,CLOSED'"),
	Entry("PowerShell's splatting", []string{"--email", "@bot"}, "fft --email '@bot'"),
	Entry("PowerShell's stop-parsing token", []string{"--%"}, "fft '--%'"),
	Entry("a number PowerShell would convert", []string{"--size", "1kb"}, "fft --size '1kb'"),
	Entry("an id that starts like a number", []string{"8f14e45f-ceea"}, "fft '8f14e45f-ceea'"),
	Entry("a leading zero", []string{"007"}, "fft '007'"),
)

var _ = DescribeTable("commandLine marks what no quoting keeps intact in every shell",
	func(value, shown string) {
		cmd := commandLine([]string{"project", "use", value})
		Expect(cmd.unportable).To(BeTrue())
		Expect(cmd.String()).To(Equal("fft project use " + shown + "  (cannot be copied safely)"))
		Expect(cmd.String()).NotTo(ContainSubstring("\x1b"))
	},
	Entry("a single quote", "it's", `'it'\''s'`),
	Entry("a backslash, which fish reads inside single quotes", `C:\tmp`, `'C:\tmp'`),
	Entry("a typographic quote, which PowerShell takes for a quote", "it’s", "'it’s'"),
	Entry("a control character, dropped from what is shown", "a\x1b[2Jb", "'a[2Jb'"),
	Entry("a newline, folded", "a\nb", "'a b'"),
	Entry("a bidi override", "a\u202eb", "'a\u202eb'"),
)

// roundTrip has shell read printf's arguments as spelled by shellQuote, and
// returns what it passed on.
func roundTrip(shell []string, printer string, values []string) []string {
	GinkgoHelper()
	quoted := make([]string, len(values))
	for i, v := range values {
		q, portable := shellQuote(v)
		Expect(portable).To(BeTrue(), "value %q", v)
		quoted[i] = q
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, shell[0], append(shell[1:], printer+" "+strings.Join(quoted, " "))...)
	cmd.Stdout = &out
	cmd.Stderr = GinkgoWriter
	Expect(cmd.Run()).To(Succeed())
	return strings.Split(strings.TrimSuffix(out.String(), "\x00"), "\x00")
}

var _ = Describe("a quoted command line pasted into a shell", func() {
	values := []string{
		"staging", "my project", "", "$HOME", "`whoami`", "a;b|c&d", "*.json", "~/x",
		"{a,b}", "OPEN,CLOSED", "@bot", "--%", "1kb", "0x10", "1.50", "007", "25", "-1",
		"8f14e45f-ceea-467a-9575-25a1b5c8b3a1", `say "hi"`, "#comment", "(x)", "ümlaut",
		"https://acme.api.fulfillmenttools.com/api?x=1&y=2",
	}

	const posixPrinter = `printf '%s\0'`

	It("reads back every value in a POSIX shell", func() {
		Expect(roundTrip([]string{"sh", "-c"}, posixPrinter, values)).To(Equal(values))
	})

	It("reads back every value in fish", func() {
		fish, err := exec.LookPath("fish")
		if err != nil {
			Skip("fish is not installed")
		}
		Expect(roundTrip([]string{fish, "--no-config", "-c"}, posixPrinter, values)).To(Equal(values))
	})

	It("reads back every value in PowerShell", func() {
		pwsh, err := exec.LookPath("pwsh")
		if err != nil {
			Skip("PowerShell is not installed")
		}
		printer := `& { foreach ($a in $args) { [Console]::Out.Write([string]$a + [char]0) } }`
		Expect(roundTrip([]string{pwsh, "-NoProfile", "-NonInteractive", "-Command"}, printer, values)).
			To(Equal(values))
	})
})
