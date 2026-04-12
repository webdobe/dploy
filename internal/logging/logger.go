// Package logging is the structured log surface for operations.
//
// It has two jobs:
//  1. write human-readable progress to stdout/stderr respecting
//     --verbose and --quiet.
//  2. write structured operation logs to .dploy/logs/<env>/<ts>.log
//     so `dploy logs <env>` can replay them later.
//
// External log sinks (HTTP endpoints, logging services) are wired
// through provider.LogSink, not through this package directly.
package logging

import (
	"fmt"
	"io"
	"os"
	"time"
)

// Logger is a small, explicit logger. No leveled API — just the calls
// the CLI actually needs.
type Logger struct {
	out     io.Writer
	err     io.Writer
	verbose bool
	quiet   bool
}

// New constructs a Logger writing to stdout/stderr.
func New(verbose, quiet bool) *Logger {
	return &Logger{
		out:     os.Stdout,
		err:     os.Stderr,
		verbose: verbose,
		quiet:   quiet,
	}
}

// Step prints the "[i/n] command" line that appears before each step.
func (l *Logger) Step(index, total int, command string) {
	if l.quiet {
		return
	}
	fmt.Fprintf(l.out, "[%d/%d] %s\n", index, total, command)
}

// Info prints a human-level message. Suppressed by --quiet.
func (l *Logger) Info(format string, args ...any) {
	if l.quiet {
		return
	}
	fmt.Fprintf(l.out, format+"\n", args...)
}

// Debug prints a verbose-only message. Suppressed unless --verbose.
func (l *Logger) Debug(format string, args ...any) {
	if !l.verbose {
		return
	}
	fmt.Fprintf(l.out, "[%s] "+format+"\n", append([]any{time.Now().Format("15:04:05")}, args...)...)
}

// Error writes an error line to stderr. Always shown.
func (l *Logger) Error(format string, args ...any) {
	fmt.Fprintf(l.err, format+"\n", args...)
}
