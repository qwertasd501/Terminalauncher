package output

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
)

const ansiReset = "\x1b[0m"

// highlight writes one log line: a bold prefix in the level color, then the whole message body in
// the same color. Callers often embed styled segments such as a bold instance name; those end with
// a reset that would also clear the level color, so paint re-applies it after every reset.
func highlight(c *color.Color, prefix string, format string, a ...any) {
	c.Printf("%s", prefix)
	// The trailing newline keeps vet from treating this as a printf wrapper and flagging every
	// caller that passes a translated (non-constant) format string.
	var b strings.Builder
	fmt.Fprintf(&b, format+"\n", a...)
	fmt.Println(paint(c, strings.TrimSuffix(b.String(), "\n")))
}

// paint wraps body in the color's escape sequence.
func paint(c *color.Color, body string) string {
	if color.NoColor || body == "" {
		return body
	}
	if !strings.Contains(body, ansiReset) {
		return c.Sprint(body)
	}
	inner := strings.TrimSuffix(c.Sprint(body), ansiReset)
	return strings.ReplaceAll(inner, ansiReset, ansiReset+reopen(c)) + ansiReset
}

// reopen returns the escape sequence that re-enables the color, e.g. "\x1b[37m".
func reopen(c *color.Color) string {
	return strings.TrimSuffix(c.Sprint(""), ansiReset)
}

// Info prints an general informational message.
func Info(format string, a ...any) {
	highlight(color.New(color.Bold, color.FgWhite), "| ", format, a...)
}

// Success prints a success information message.
//
// Indicates a command or task has successfully completed.
func Success(format string, a ...any) {
	highlight(color.New(color.Bold, color.FgGreen), "| ", format, a...)
}

// Warning prints a cautionary message.
//
// Indicates that there may be an issue.
func Warning(format string, a ...any) {
	highlight(color.New(color.Bold, color.FgYellow), "| "+Translate("launcher.warning")+": ", format, a...)
}

// Debug prints a debug message.
//
// Used to print information messages useful for debugging the launcher.
func Debug(format string, a ...any) {
	highlight(color.New(color.Bold, color.FgMagenta), "| "+Translate("launcher.debug")+": ", format, a...)
}

// Error prints an error message.
//
// Indicates a fatal error.
func Error(format string, a ...any) {
	highlight(color.New(color.Bold, color.FgRed), "| "+Translate("launcher.error")+": ", format, a...)
}

// Tip prints a tip message.
//
// Indicates an action that should be performed.
func Tip(format string, a ...any) {
	highlight(color.New(color.Bold, color.FgYellow), "| "+Translate("launcher.tip")+": ", format, a...)
}
