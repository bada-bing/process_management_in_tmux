package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func tmux(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("tmux %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// currentSession reads the session from the calling pane rather than from the
// attached client, so it is correct however the tool was invoked.
func currentSession() (string, error) {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return "", fmt.Errorf("not inside tmux - name the project with -p")
	}
	return tmux("display-message", "-p", "-t", pane, "#{session_name}")
}

func sessionExists(session string) bool {
	// -t= is an exact match; -t alone also matches a longer name.
	_, err := tmux("has-session", "-t="+session)
	return err == nil
}

// findWindows returns the ids of every window with this name. tmux permits
// duplicate names and resolves an ambiguous target unpredictably, so a name is
// never used as the target of a destructive call - the id always is. Returning
// all of them is what lets -f leave exactly one window behind.
func findWindows(session, name string) ([]string, error) {
	out, err := tmux("list-windows", "-t="+session, "-F", "#{window_id}\t#{window_name}")
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, line := range strings.Split(out, "\n") {
		id, wname, ok := strings.Cut(line, "\t")
		if ok && wname == name {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func killWindow(id string) error {
	_, err := tmux("kill-window", "-t", id)
	return err
}

// newWindow opens a window and types the command into it, which is exactly
// what starting a process by hand looks like: tmux is given no command, so the
// pane runs default-shell - a login, interactive shell - and the command goes
// in as keys.
//
// That the shell stays is the whole point. A command that exits leaves a
// prompt in the right directory with its own error above it, so the window can
// be worked in rather than only read, and Ctrl-D closes it like any other.
// Nothing has to be kept alive artificially, so remain-on-exit is not used and
// no pane is ever dead.
//
// The keys are sent without waiting for the shell: tmux buffers them into the
// pane until the shell reads them, so there is nothing to race. Both commands
// go in one invocation, so nothing can land between the text and the Enter that
// submits it. -l sends the command literally, since a declaration that happened
// to read like a key name would otherwise be interpreted as one.
func newWindow(session, name, cwd, command string, env map[string]string) (string, error) {
	id, err := tmux("new-window", "-d", "-P", "-F", "#{window_id}",
		"-t", session+":", "-n", name, "-c", cwd)
	if err != nil {
		return "", err
	}

	if _, err := tmux("send-keys", "-t", id, "-l", "--", withEnv(withState(command), env),
		";", "send-keys", "-t", id, "Enter"); err != nil {
		return "", err
	}
	return id, nil
}

// paneState reads @proc_state from the window's first pane, the process's own;
// a pane split into it later comes after it. It is false when the window is gone.
func paneState(window string) (string, bool) {
	out, err := tmux("list-panes", "-t", window, "-F", "#{"+stateOption+"}")
	if err != nil {
		return "", false
	}
	first, _, _ := strings.Cut(out, "\n")
	return first, true
}
