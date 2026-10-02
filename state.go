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

// watch waits for the command to record `running`, then watches it for
// `exited`, and returns the state it ends on: empty if the shell never reached
// the command. It is false when the window is gone, as a bare exit takes it.
func watch(id string) (string, bool) {
	started := false
	deadline := time.Now().Add(startBudget)

	for {
		state, ok := paneState(id)
		if !ok || state == "exited" {
			return state, ok
		}
		if state == "running" && !started {
			started = true
			deadline = time.Now().Add(watchWindow)
		}

		if time.Now().After(deadline) {
			return state, true
		}
		time.Sleep(poll)
	}
}

// windowsState sums up the windows carrying a name: `running` if any of them
// is, `exited` if all of them are, and empty otherwise.
func windowsState(ids []string) string {
	exited := len(ids) > 0
	for _, id := range ids {
		switch state, _ := paneState(id); state {
		case "running":
			return "running"
		case "exited":
		default:
			exited = false
		}
	}
	if exited {
		return "exited"
	}
	return ""
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
func report(state string, ok bool) {
	switch {
	case !ok:
		fmt.Println("  exited, and ended the window's shell with it")
	case state == "":
		fmt.Printf("  not started yet - the window's shell has not reached the command after %s\n", startBudget)
	default:
		fmt.Println("  " + state)
	}
}
