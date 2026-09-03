## What this changes

<!-- One or two sentences. Link the issue if there is one: "Fixes #12". -->

## Why

<!-- The reasoning, if it is not obvious from the change itself. -->

## Before / after

<!--
For anything the user can see, paste both. A screenshot of the screen, or the
output of a command, is the fastest way to review a rendering change.
-->

## Checklist

- [ ] `go build ./... && go vet ./... && go test -race ./...` passes
- [ ] `golangci-lint run ./...` is clean
- [ ] `gofmt -l .` prints nothing
- [ ] Tests are in this PR, not promised for a later one
- [ ] If it touches staging: a test asserts the working tree is unchanged
- [ ] If it adds a theme key: both built-in themes set it
- [ ] If it adds a dependency: the PR says why, and the license is permissive
