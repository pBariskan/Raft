# M0 — Setup

Goal: an empty but correct Go project skeleton, so every later milestone only adds code and tests.

Done when: `go test ./...` passes locally and in GitHub Actions, and the folders and docs below
exist.

- [x] Go module `github.com/pBariskan/Raft` (Go 1.24).
- [x] Package skeletons with a doc comment stating each package's job: `raft/`, `sim/`, `kv/`,
      `storage/`, `transport/`. (`cmd/`, `wasm/`, `web/` are created when their milestones start.)
- [x] `.gitignore` for Go build output, coverage files and editor/OS files.
- [x] GitHub Actions workflow: `gofmt` check, `go vet ./...`, `go test -race ./...` on every push
      and pull request.
- [x] `docs/bugs.md` (empty log with the entry format) and `docs/learning/` (index README).
- [x] `CLAUDE.md` commands and "where things stand" updated; M0 ticked in `PROJECT.md`.

Choices *(Claude's call)*:
- Go 1.24: the toolchain already in the environment; CI reads the version from `go.mod`.
- `-race` in CI from day one: `raft/` has no goroutines, but `transport/` and `cmd/` will.
