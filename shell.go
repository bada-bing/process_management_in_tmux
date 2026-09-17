package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// withEnv prefixes a command with the variables declared for it. They go
// inside the login shell rather than into the pane's environment so that they
// apply after .zprofile, which would otherwise overwrite a name it exports
// too. A config states what a process is configured with, so it wins.
func withEnv(command string, env map[string]string) string {
	if len(env) == 0 {
		return command
	}

	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names) // a map iterates in random order; the command must not

	// export rather than a `NAME=value command` prefix, which would apply
	// only to the first command of a shell line.
	var b strings.Builder
	b.WriteString("export")
	for _, name := range names {
		b.WriteString(" ")
		b.WriteString(name)
		b.WriteString("=")
		b.WriteString(shellQuote(env[name]))
	}
	b.WriteString("; ")
	b.WriteString(command)

	return b.String()
}

func loginShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// shellQuote wraps a string so a shell reads it as one literal argument.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// bootstrapSession delegates session creation to the script that already owns
// session shape. It is idempotent, so it is safe to call unconditionally.
func bootstrapSession(session, cwd, layout string) error {
	script := filepath.Join(scriptsDir(), "tmux", "bootstrap_session.sh")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("bootstrap script not found at %s", script)
	}

	cmd := exec.Command(script, session, cwd, layout)
	var errb bytes.Buffer
	cmd.Stderr = &errb

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bootstrap_session.sh %s: %s", session, strings.TrimSpace(errb.String()))
	}
	return nil
}
func scriptsDir() string {
	if d := os.Getenv("SCRIPTS_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Developer/toolbox/scripts")
}
