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

// newWindow creates the window detached, so starting a process never moves the
// client - including when it lands in a session other than the current one.
//
// The command runs under a login shell. tmux hands a window only the server's
// environment, which carries neither SCRIPTS_DIR nor the PATH the tools are
// on, and a login shell is what mprocs effectively inherited from the shell
// that launched it. Reading the environment here instead would make a
// process depend on where proc happened to be run from.
func newWindow(session, name, cwd, command string, env map[string]string) error {
	args := []string{"new-window", "-d", "-t", session + ":", "-n", name, "-c", cwd}
	args = append(args, loginShell()+" -l -c "+shellQuote(withEnv(command, env)))

	_, err := tmux(args...)
	return err
}
