// proc runs the processes a project needs, one tmux window each.
//
// A process is addressed by name within a project, and the project is either
// the session the command was run from or named with -p. Where a process runs
// is therefore predictable from its project alone, wherever it was started.
package main

import (
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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

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

	for i, s := range r.order {
		// The process named on the command line is resolved last, and it is
		// the only one an existing window refuses rather than satisfies.
		named := i == len(r.order)-1
		if err := create(s, named, args.force); err != nil {
			return err
		}
	}

	return nil
}

// create makes a window for one step unless something already occupies the
// name. What that means depends on how the step got here: a requirement is
// satisfied by whatever is already there, while the process asked for is
// refused unless -f says to replace it.
func create(s step, named, force bool) error {
	var existing []string

	if sessionExists(s.cfg.Session) {
		ids, err := findWindows(s.cfg.Session, s.name)
		if err != nil {
			return err
		}
		existing = ids
	}

	if len(existing) > 0 {
		if !named {
			return nil
		}
		if !force {
			return fmt.Errorf("window %q already exists in session %q - use -f to replace it",
				s.name, s.cfg.Session)
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

	p, err := s.cfg.proc(s.name)
	if err != nil {
		return err
	}
	if err := newWindow(s.cfg.Session, s.name, s.cfg.procCwd(p), p.Shell, p.Env); err != nil {
		return err
	}

	fmt.Printf("started %s:%s\n", s.cfg.Session, s.name)
	return nil
}

// ensureSession creates the session a process runs in, delegating to the
// script that owns session shape. It is idempotent, so calling it is cheap.
func ensureSession(cfg *Config) error {
	if sessionExists(cfg.Session) {
		return nil
	}
	return bootstrapSession(cfg.Session, cfg.Cwd, cfg.project)
}
