# Overview

Gracefully is a GoLang library intended to facilitate building services that are interruptable. After having started using Go to develop products, I noticed that this was constant boilerplate for a microservice-based architecture and decided to standardize it.

# Quickstart

Add to your go.mod file:

```
require github.com/wojnosystems/gracefully v1.0.0
```

```go
package main

import (
	"context"
	"github.com/wojnosystems/gracefully"
)

func main() {
    // Service Setup
    sm := gracefully.New()
    sm.AddSignaler(gracefully.DefaultSignals())
    err := sm.Run(func(ctx context.Context) error {
        // Do work
        // put your code here
        <-ctx.Done()
        return nil // or return the error
    })
    // Cleanup
    if err != nil {
        panic(err)
    }
}
```

The above example will listen for incoming signals from the operating system. ctx.Done() will be signalled when that signal arrives. If the signal is a SIGHUP, the function in Run will be restarted for you. If it's SIGINT or SIGTERM, the function will not be called again.

# How it's laid out

Services typically have a few components:

1. Configuration
1. Listening for signals
1. Listening for control sockets
1. Processing
   1. Iterating in a loop
   1. Launching a loop/forking processes
1. Waiting for signals/controls
1. Cleaning up

## State Machine

```
Unconfigured -> New -> Running <-> Restarting
                          V
                        Dying -> Dead (cleanup)
```

# How to use it

Typically, you run these services in main. Gracefully will let you configure it like middleware, adding components that are automatically handled properly.

When you create a New ServiceManager, it has no controls enabled by default. You add them in by using the "AddSignaler" method. Two signalers are provided by default:

* Signals: allows the ServiceManager to be restarted or stopped by listening to operating signals like SIGINT/SIGTERM/SIGHUP
* ContextSignal: allows the ServiceManager to be stopped by calling "Stop" and restarted by calling "Restart"

You can use ContextSignal to build other signalers or design your own by implementing the SelectSignaler interface and passing it to the "AddSignaler" method.

# Copyright

2019 © Christopher Wojno, all rights reserved

# License

See license file included with repository under "LICENSE"