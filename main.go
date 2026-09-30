// proc runs the processes a project needs, one tmux window each.
//
// A process is addressed by name within a project, and the project is either
// the session the command was run from or named with -p. Where a process runs
// is therefore predictable from its project alone, wherever it was started.
package main

import (
	"errors"
	"fmt"
	"os"
)

const usage = `proc runs the processes a project needs, one tmux window each.

usage:
  proc start <process> [-p <project>] [-f]

  <process>  a process declared in the project's config
  -p         the project; defaults to the name of the session you are in
  -f         replace the window if one already carries that name, killing
             whatever is running in it

examples:
  proc start remote                  the project is the current session
  proc start remote -f               bounce it
  proc start remote -p shop
  proc start zookeeper -p shared

A process always runs in its project's session, whatever session you start it
from, so its address is predictable: <project>:<process>.

A process runs in a window like one you opened yourself, with the command
typed into it, so a command that exits leaves a prompt and its own output
behind rather than taking the window with it.

Declarations live in one file per project, named after it:
  $PROC_CONFIG_DIR, else $ENV_DIR/proc, else ~/Developer/toolbox/private/env/proc
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "start":
		err = runStart(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}

	if err != nil {
		// A failure proc has already described in full says nothing more here.
		if !errors.Is(err, errReported) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

// errReported marks a failure whose explanation is already on screen, so the
// exit code can be honest without the reason being given twice.
var errReported = errors.New("reported")

type startArgs struct {
	name    string
	project string
	force   bool
}

// parseStart accepts flags in any position, so `start remote -f` reads the
// same as `start -f remote`.
func parseStart(argv []string) (startArgs, error) {
	var a startArgs

	for i := 0; i < len(argv); i++ {
		switch arg := argv[i]; arg {
		case "-f", "--force":
			a.force = true
		case "-p", "--project":
			i++
			if i >= len(argv) {
				return a, fmt.Errorf("-p needs a project name")
			}
			a.project = argv[i]
		default:
			if len(arg) > 1 && arg[0] == '-' {
				return a, fmt.Errorf("unknown flag %q", arg)
			}
			if a.name != "" {
				// The project is a flag, not a position - a mistake worth
				// answering with the command that was meant.
				return a, fmt.Errorf("start takes one process name, got %q and %q\n"+
					"did you mean: proc start %s -p %s", a.name, arg, arg, a.name)
			}
			a.name = arg
		}
	}

	if a.name == "" {
		return a, fmt.Errorf("start needs a process name")
	}
	return a, nil
}

func runStart(argv []string) error {
	args, err := parseStart(argv)
	if err != nil {
		return err
	}

	project := args.project
	if project == "" {
		project, err = currentSession()
		if err != nil {
			return err
		}
	}

	// Resolve the whole graph before creating anything, so a cycle or a bad
	// address fails while no window exists rather than half way through.
	r := newResolver()
	if err := r.resolve(project, args.name, nil); err != nil {
		return err
	}

	last := len(r.order) - 1
	for i, s := range r.order {
		// The process named on the command line is resolved last, and it is
		// the only one an existing window refuses rather than satisfies.
		named := i == last
		err := create(s, named, args.force)
		if err == nil {
			continue
		}
		if named {
			return err
		}
		// A requirement that failed is reported with what it cost, since the
		// process asked for is the only reason any of this was started.
		return fmt.Errorf("%w\n%s:%s was not started", err, r.order[last].cfg.project, r.order[last].name)
	}

	return nil
}

// create makes a window for one step unless the process is already up. What
// that means depends on how the step got here and on what it declares.
//
// A requirement is satisfied by a window with a command still running in it,
// while the process asked for is refused unless -f says to replace it. Whatever
// create does, it then says what became of it: `started` and `found` are
// actions proc took, `running` and `exited` are states it went and looked up.
func create(s step, named, force bool) error {
	addr := s.cfg.project + ":" + s.name

	p, err := s.cfg.proc(s.name)
	if err != nil {
		return err
	}

	shell := shellName()

	var existing []string

	if sessionExists(s.cfg.project) {
		ids, err := findWindows(s.cfg.project, s.name)
		if err != nil {
			return err
		}
		existing = ids
	}

	if len(existing) > 0 {
		if !named {
			return found(addr, existing, shell)
		}
		if !force {
			// A window outlives its command, so it carries the name whether
			// anything is running in it or not. The refusal says which it is.
			state := "already exists"
			if _, live := liveWindow(existing, shell); !live {
				state = "is there with nothing running in it"
			}
			return fmt.Errorf("window %q in session %q %s - use -f to replace it",
				s.name, s.cfg.project, state)
		}
		for _, id := range existing {
			if err := killWindow(id); err != nil {
				return err
			}
		}
	}

	if err := ensureSession(s.cfg); err != nil {
		return err
	}

	id, err := newWindow(s.cfg.project, s.name, s.cfg.procCwd(p), p.Shell, p.Env)
	if err != nil {
		return err
	}
	fmt.Printf("started %s\n", addr)

	alive := running(id, shell)
	report(alive, id)
	if alive {
		return nil
	}
	if named {
		return errReported
	}
	return fmt.Errorf("requirement %s is not running", addr)
}

// found reports a requirement met by a window that was already there. The
// window has to have a command running in it - a name on a window proves
// nothing, and now that a window outlives its command it proves less than ever.
func found(addr string, existing []string, shell string) error {
	fmt.Printf("found %s\n", addr)

	if _, ok := liveWindow(existing, shell); !ok {
		report(false, existing[0])
		return fmt.Errorf("requirement %s is not running", addr)
	}
	fmt.Println("  running")
	return nil
}

// ensureSession creates the session a process runs in, delegating to the
// script that owns session shape. It is idempotent, so calling it is cheap.
func ensureSession(cfg *Config) error {
	if sessionExists(cfg.project) {
		return nil
	}
	return bootstrapSession(cfg.project, cfg.Cwd, cfg.project)
}
