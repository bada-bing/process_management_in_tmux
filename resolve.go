package main

import (
	"fmt"
	"slices"
	"strings"
)

// step is one window to create. It holds the loaded config rather than copies
// of its fields, so nothing is read from disk twice.
type step struct {
	cfg  *Config
	name string
}

// resolver walks the requirement graph. It reads configs and makes no tmux
// call, so a bad declaration is caught before any window exists.
type resolver struct {
	resolved map[string]bool // addresses already worked out and placed in order
	order    []step          // what to create, requirements before dependents
}

func newResolver() *resolver {
	return &resolver{resolved: map[string]bool{}}
}

// resolve places a process in the order, its requirements first.
//
// ancestors is the chain of calls still open above this one - not a route to
// walk, and not a record of what ran. An address appearing in it is waiting on
// itself, which is a cycle. An address in resolved was reached down some other
// branch that has since finished, which is a shared requirement and fine.
func (r *resolver) resolve(project, name string, ancestors []string) error {
	addr := project + ":" + name

	if slices.Contains(ancestors, addr) {
		chain := append(slices.Clone(ancestors), addr)
		return fmt.Errorf("requirement cycle: %s", strings.Join(chain, " -> "))
	}
	if r.resolved[addr] {
		return nil
	}

	cfg, err := loadConfig(project)
	if err != nil {
		return err
	}
	p, err := cfg.proc(name)
	if err != nil {
		return err
	}

	// A sibling may overwrite this slice's tail on its own append, which is
	// harmless: by then this branch has finished and nobody reads it again.
	within := append(ancestors, addr)

	for _, req := range p.Requires {
		reqProject, reqName, ok := strings.Cut(req, ":")
		if !ok {
			return fmt.Errorf("%s requires %q, which is not a <project>:<process> address",
				addr, req)
		}
		if err := r.resolve(reqProject, reqName, within); err != nil {
			return err
		}
	}

	r.resolved[addr] = true
	r.order = append(r.order, step{cfg: cfg, name: name})

	return nil
}
