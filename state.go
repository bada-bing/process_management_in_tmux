package main

import (
	"fmt"
	"time"
)

// startBudget is how long proc waits to see a typed command reach the pane's
// foreground. It covers the shell starting up too - about 325ms here, since the
// window is made and the keys sent in the same breath - because until the shell
// has run the command, a pane at a prompt looks exactly like a pane whose
// command has finished. Three seconds leaves room for a cold start.
const startBudget = 3 * time.Second

// watchWindow is how long a command is watched once it is running. It is a
// compromise, not supervision: everything that fails at startup - a missing
// binary, a refused credential, a port already held - is gone well inside it.
// A process that dies later slips through, and its window is still there to be
// read whenever that is noticed.
const watchWindow = 750 * time.Millisecond

const poll = 25 * time.Millisecond

// running answers whether the command proc typed is still the pane's
// foreground process.
//
// It returns as soon as a command that started has finished, so a failure is
// reported the moment it happens. A pane that never leaves the shell is the
// ambiguous case - a command too quick to catch, or one that never ran - and
// proc deliberately does not distinguish them: the question is whether
// anything is running, and the answer to that is no either way.
func running(id, shell string) bool {
	started := false
	deadline := time.Now().Add(startBudget)

	for {
		switch cur := paneCommand(id); {
		case cur == "":
			// No pane to ask. A command that ends the shell itself, such as a
			// bare exit, takes the window with it exactly as it would if you
			// had typed it.
			return false
		case cur != shell:
			if !started {
				started = true
				deadline = time.Now().Add(watchWindow)
			}
		case started:
			return false
		}

		if time.Now().After(deadline) {
			return started
		}
		time.Sleep(poll)
	}
}

// liveWindow picks the first of several windows carrying a name that has a
// command running in it. A window outlives its command now, so the name alone
// answers nothing.
func liveWindow(ids []string, shell string) (string, bool) {
	for _, id := range ids {
		if cur := paneCommand(id); cur != "" && cur != shell {
			return id, true
		}
	}
	return "", false
}

// report prints what proc found, indented under the line saying what proc did.
// The two vocabularies are kept apart on purpose: `started` and `found` are
// actions proc took, `running` and `exited` are states of the system.
//
// What the command printed is not repeated here. It is in the window, which is
// the one place it is complete and the one place it stays, and capture-pane is
// how anything - me or an agent - reads a pane. proc says which of them to read
// by naming the process; saying it twice is what made the report worth
// distrusting.
func report(alive bool, id string) {
	if alive {
		fmt.Println("  running")
		return
	}
	if paneCommand(id) == "" {
		fmt.Println("  exited, and ended the window's shell with it")
		return
	}
	fmt.Println("  exited")
}
