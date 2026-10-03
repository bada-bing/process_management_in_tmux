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
  -f         restart the process if it is running, replacing its window

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
		// the only one -f restarts.
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
		target := r.order[last].cfg.project + ":" + r.order[last].name
		var down requirementDown
		if errors.As(err, &down) {
			return fmt.Errorf("%s was not started: its requirement %s %s", target, down.addr, down.why)
		}
		return fmt.Errorf("%w\n%s was not started", err, target)
	}

	return nil
}

// create makes a window for one step unless the process is already up. A
// window whose command has exited is replaced, whether the process was named
// or required. Whatever create does, it then says what became of it: `started`
// and `found` are actions proc took, `running` and `exited` are states it read.
func create(s step, named, force bool) error {
	addr := s.cfg.project + ":" + s.name

	p, err := s.cfg.proc(s.name)
	if err != nil {
		return err
	}

	var existing []string

	if sessionExists(s.cfg.project) {
		ids, err := findWindows(s.cfg.project, s.name)
		if err != nil {
			return err
		}
		existing = ids
	}

	state := windowsState(existing)

	switch {
	case len(existing) == 0, state == "exited":
	case state == "":
		// proc cannot say what runs in a window it did not open, so it neither
		// keeps the window nor kills it unless told to.
		if named && force {
			break
		}
		msg := fmt.Sprintf("a window named %s is already open in %s, but proc did not open it",
			s.name, s.cfg.project)
		if named {
			return fmt.Errorf("%s - use -f to replace it", msg)
		}
		return fmt.Errorf("%s - replace it with: proc start %s -p %s -f", msg, s.name, s.cfg.project)
	case !named, !force:
		found(addr)
		if named {
			fmt.Printf("%s is already running - use -f to restart it\n", addr)
		}
		return nil
	}

	for _, id := range existing {
		if err := killWindow(id); err != nil {
			return err
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

	state, ok := watch(id)
	report(addr, state, ok)
	if state == "running" {
		return nil
	}
	if named {
		return errReported
	}
	why := "exited"
	if ok && state == "" {
		why = "has not started yet"
	}
	return requirementDown{addr, why}
}

// requirementDown is a requirement that was started but is not running, which
// the start reports in the words of the process that needed it.
type requirementDown struct{ addr, why string }

func (d requirementDown) Error() string { return d.addr + " " + d.why }

// found reports a process met by a window already running it.
func found(addr string) {
	fmt.Printf("found %s\n", addr)
	fmt.Println("  running")
}

// ensureSession creates the session a process runs in, delegating to the
// script that owns session shape. It is idempotent, so calling it is cheap.
func ensureSession(cfg *Config) error {
	if sessionExists(cfg.project) {
		return nil
	}
	return bootstrapSession(cfg.project, cfg.Cwd, cfg.project)
}
