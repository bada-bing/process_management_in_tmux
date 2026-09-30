package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Proc is one declared process. The shape mirrors the mprocs config it grew
// out of, so an existing file ports over once its mprocs-only keys are gone.
type Proc struct {
	Cwd   string            `yaml:"cwd"`
	Shell string            `yaml:"shell"`
	Env   map[string]string `yaml:"env"`
	// Requires names processes this one needs, each as a full
	// <project>:<process> address so nothing has to be resolved by rule.
	Requires []string `yaml:"requires"`
}

// Config is one project's declaration. A project is the unit everything is
// keyed on: it names the config file and the session its processes run in, so
// an address <project>:<process> is also the tmux target.
type Config struct {
	// Cwd of the project. Defaults to the directory named after the project
	// under SRC_PATH.
	Cwd   string          `yaml:"cwd"`
	Procs map[string]Proc `yaml:"procs"`

	project string
	path    string
}

// configDir holds one file per project, named after it.
func configDir() string {
	if d := os.Getenv("PROC_CONFIG_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("ENV_DIR"); d != "" {
		return filepath.Join(d, "proc")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Developer/toolbox/private/env/proc")
}

func loadConfig(project string) (*Config, error) {
	path := filepath.Join(configDir(), project+".yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no config for project %q at %s", project, path)
		}
		return nil, err
	}

	// A key the schema does not have is refused rather than dropped, so a
	// leftover or mistyped key cannot silently change nothing.
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	c.project = project
	c.path = path

	if c.Cwd == "" {
		dir, err := resolveProjectDir(project)
		if err != nil {
			return nil, err
		}
		c.Cwd = dir
	}
	c.Cwd = expandHome(c.Cwd)

	return &c, nil
}

// proc returns one declared process, listing what is available when the name
// is not one of them - a name that resolves to nothing is an error, never a
// guess.
func (c *Config) proc(name string) (Proc, error) {
	p, ok := c.Procs[name]
	if !ok {
		return Proc{}, fmt.Errorf("project %q has no process %q\ndeclared: %s",
			c.project, name, strings.Join(c.procNames(), ", "))
	}
	if p.Shell == "" {
		return Proc{}, fmt.Errorf("%s: process %q declares no shell command", c.path, name)
	}
	return p, nil
}

func (c *Config) procNames() []string {
	names := make([]string, 0, len(c.Procs))
	for n := range c.Procs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// procCwd resolves a process's directory against the project's.
func (c *Config) procCwd(p Proc) string {
	if p.Cwd == "" {
		return c.Cwd
	}
	if filepath.IsAbs(p.Cwd) {
		return p.Cwd
	}
	return filepath.Clean(filepath.Join(c.Cwd, p.Cwd))
}

// resolveProjectDir looks for a directory named after the project under each
// root in SRC_PATH, the same lookup task_session.sh performs.
func resolveProjectDir(project string) (string, error) {
	srcPath := os.Getenv("SRC_PATH")
	if srcPath == "" {
		home, _ := os.UserHomeDir()
		srcPath = filepath.Join(home, "Developer/src")
	}

	roots := strings.Split(srcPath, ":")
	for _, root := range roots {
		dir := filepath.Join(expandHome(root), project)
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir, nil
		}
	}

	return "", fmt.Errorf("no directory named %q under %s - declare cwd in the config",
		project, srcPath)
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}
