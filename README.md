# proc

Starts a tmux window running a predefined command. It supervises nothing —
tmux owns the process, and `proc` exits once the window exists.

`proc --help` covers the command surface. The design lives on
`10x_18-Design_Process_Management_in_Tmux`.

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

`proc start host-bff -p shop`, where `host-bff` requires
`shared:zookeeper`:

    order[0]  session  shared_processes
              name     zookeeper
              cwd      /Users/miki
              shell    kubectl port-forward -n zookeeper svc/zookeeper 2181:2181

    order[1]  session  shop
              name     host-bff
              cwd      .../shop/packages/host-bff
              shell    pnpm dev

Directories are already absolute, so the create pass never reads config again.
`order[0]` becomes:

    tmux new-window -d -t shared_processes: -n zookeeper \
      -c /Users/miki \
      "/bin/zsh -l -c 'kubectl port-forward -n zookeeper svc/zookeeper 2181:2181'"

The login shell is there because a tmux window inherits only the server's
environment, which has neither `SCRIPTS_DIR` nor the PATH the tools sit on.

The last entry is always the process named on the command line, which is how
the create pass knows to treat it differently: an existing window satisfies a
requirement, but refuses the process you asked for unless `-f` says to replace
it.
