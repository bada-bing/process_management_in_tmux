# proc

Starts a tmux window running a predefined command. It supervises nothing —
tmux owns the process, and `proc` exits once the window exists. It does look
once, though, at what became of the command it typed.

`proc --help` covers the command surface. The design lives on
`10x_18-Design_Process_Management_in_Tmux`.

    go build -o ~/.local/bin/proc .

## A window like any other

tmux is given no command, so the pane runs `default-shell` — a login,
interactive shell — and the command goes in as keys:

    tmux new-window -d -P -F '#{window_id}' -t shared: -n zookeeper -c /Users/miki \
      ";" send-keys -t @31 -l -- "kubectl port-forward -n zookeeper svc/zookeeper 2181:2181" \
      ";" send-keys -t @31 Enter

That the shell stays is the whole point. A command that exits leaves a prompt
in the right directory with its own error above it, so the window can be worked
in rather than only read: press up to re-run it, change a flag, look around.
`Ctrl-D` closes it like anything else.

The keys are sent without waiting for the shell — tmux buffers them into the
pane until it reads them, so there is nothing to race.

Previously the command *was* the pane's process, so it took the window with it
when it died — no window, no output, no trace, and the first symptom was
whatever depended on it, hanging. Keeping the pane alive artificially
(`remain-on-exit`) fixes the trace and not the rest: a dead pane can be read but
not used, and it swallows every key you type at it.

Both tmux commands go in one invocation, so nothing can land between the text
and the Enter that submits it. `-l` sends the command literally, since a
declaration that happened to read like a key name would otherwise be
interpreted as one.

## What proc did, and what is true

Two vocabularies, deliberately kept apart. `started` and `found` are actions
`proc` took; the indented line under them is a state it went and looked up.

    started probe:survivor               proc opened a window and typed the command
      running                            …and the command has not exited

    started probe:instant-death
      exited                             …and the command has
    read why: tmux capture-pane -p -J -S -50 -t probe:instant-death

    started probe:slow-shell
      not started yet - …                the shell has not reached the command

    found shared:zookeeper               proc created nothing; it was already there
      running

A line under the report says what follows from it: the command that reads a
failed process's window, a process asked for that is already running, or a
requirement that stopped the start:

    probe:survivor is already running - use -f to restart it
    probe:needs-corpse was not started: its requirement probe:instant-death exited

`started` alone was true and useless: it reports that a window was created and
a command sent, and a reader takes it to mean the process is running.

`running` means the command has not exited, not that it is ready. A
port-forward that never connects reads `running` all the same; whether a
process is serving is not answered here.

### The signal

The typed line records the command's state in the `@proc_state` option of the
window's first pane — `running` before the command starts, `exited` from an
`always` block however it ends. `proc` reads that option and nothing else: the
foreground command is not a signal, since whatever shell startup runs is
briefly in the foreground too.

### Why there is a clock at all

Neither number has anything to do with how long an app takes to be *useful* — a
dev server that needs forty seconds to serve reads `running` throughout, from
its first millisecond.

`startBudget` (3s) is how long `proc` waits for `running`, and it covers the
shell's own startup — about 325ms here — during which the state is still unset.

`watchWindow` (750ms) is how long a command is watched once it *is* running, and
it is what catches a failure at startup: a missing binary, a refused credential,
a port already held. A process that dies later is reported `running` and `proc`
is wrong; it has stopped watching by then. That is the price of supervising
nothing, and it is why the window staying put matters.

### Where the output is

In the window, which is the one place it is complete and the one place it
stays. `proc` does not repeat it: the report names the process, and naming it is
an address, which a failure spells out as the command that reads it —

    tmux capture-pane -p -J -S -200 -t probe:instant-death

— so anything that can read a pane, me or an agent, reads the same bytes. `proc`
reads no output at all; `@proc_state` is its only question.

## Two passes

A start is fully resolved before anything is created:

    resolve   read configs, order the requirements     makes no tmux call
    create    walk that order, make the missing windows

The split is what makes a bad declaration harmless. A cycle or an unknown
address fails while nothing exists, rather than after two windows are up.

## Resolving

Three values, and two of them read as something they are not:

    ancestors   the calls open above this one     shrinks   not a route to walk
    resolved    addresses already worked out      grows     nothing was started
    order       what to create, in sequence       grows     the plan

`ancestors` is backward-looking — how this call was reached, not where it is
going. It is a parameter, so each call hands its children a longer copy and the
shrinking happens by returning. `resolved` means a config was read and placed
in `order`; the first pass starts nothing at all.

## A cycle is not a shared requirement

Both revisit an address. What separates them is whether the first visit
*finished*.

Shared — `ui` needs `bff` and `worker`, both need `zk`:

    ui         ancestors=[]
      bff      ancestors=[ui]
        zk     ancestors=[ui,bff]   no requirements, so it finishes here
      worker   ancestors=[ui]
        zk     not in ancestors, is in resolved -> already done, skip

Cycle — `ui` needs `bff`, `bff` needs `zk`, `zk` needs `bff`:

    ui         ancestors=[]
      bff      ancestors=[ui]
        zk     ancestors=[ui,bff]
          bff  bff is in ancestors -> it never finished, it is waiting on this

    requirement cycle: ui -> bff -> zk -> bff

Every call ends the same way: when its loop over requirements finishes. A
process with no requirements has an empty loop, which is why the deepest one
finishes first and `order` comes out requirements before dependents.

## What order holds

`proc start dev:frontend -p project_1`, where `dev:frontend` requires
`shared:zookeeper`:

    order[0]  session  shared
              name     zookeeper
              cwd      /Users/miki
              shell    kubectl port-forward -n zookeeper svc/zookeeper 2181:2181

    order[1]  session  project_1
              name     dev:frontend
              cwd      .../project_1/packages/frontend
              shell    pnpm dev

Directories are already absolute, so the create pass never reads config again.
`order[0]` becomes:

    tmux new-window -d -P -F '#{window_id}' -t shared: -n zookeeper \
      -c /Users/miki

…followed by the command sent as keys. The window inherits only the tmux
server's environment, which has neither `SCRIPTS_DIR` nor the PATH the tools sit
on; the shell being a login shell is what repairs that.

The last entry is always the process named on the command line, which is how
the create pass knows to treat it differently: a requirement is satisfied by
a window with a command running in it, and so is the process you asked for,
unless `-f` says to restart it.

A window whose command has `exited` is replaced and started again, whether its
process was named or required. It carries the name, but the name was never the
question.

A window carrying the name with no `@proc_state` at all is one `proc` did not
open, so `proc` cannot say what runs in it. It is refused rather than kept or
killed, and `-f` replaces it when it is the process you asked for.
