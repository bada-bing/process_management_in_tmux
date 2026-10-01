package main

import (
	"fmt"
	"time"
)

// startBudget is how long proc waits for the typed line to record `running`.
// It covers the shell starting up, about 325ms here; three seconds leaves room
// for a cold start.
const startBudget = 3 * time.Second

// watchWindow is how long a running command is watched for `exited`. It is a
// compromise, not supervision: a failure at startup - a missing binary, a
// refused credential, a port already held - falls inside it, a later one does
// not, and the window is there to be read whenever that is noticed.
const watchWindow = 750 * time.Millisecond

const poll = 25 * time.Millisecond

// running waits for the command to record `running`, then watches it for
// `exited`, and answers whether it is still running at the end. A window that
// is gone - a command such as a bare exit takes it with it - is not running.
func running(id string) bool {
	started := false
	deadline := time.Now().Add(startBudget)

	for {
		state, ok := paneState(id)
		switch {
		case !ok, state == "exited":
			return false
		case state == "running" && !started:
			started = true
			deadline = time.Now().Add(watchWindow)
		}

		if time.Now().After(deadline) {
			return started
		}
		time.Sleep(poll)
	}
}

// liveWindow picks the first of several windows carrying a name whose command
// is running.
func liveWindow(ids []string) (string, bool) {
	for _, id := range ids {
		if state, _ := paneState(id); state == "running" {
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
	if _, ok := paneState(id); !ok {
		fmt.Println("  exited, and ended the window's shell with it")
		return
	}
	fmt.Println("  exited")
}
