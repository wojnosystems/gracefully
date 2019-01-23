# Overview

Gracefully is a GoLang library intended to facilitate building services that are interruptable. After having started using Go to develop products, I noticed that this was constant boilerplate for a microservice-based architecture and decided to standardize it.

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

# How to use it

Typically, you run these services in main. Gracefully will let you configure it like middleware, adding components that are automatically handled properly