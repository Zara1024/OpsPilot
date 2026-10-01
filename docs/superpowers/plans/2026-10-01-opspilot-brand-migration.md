# OpsPilot Brand Migration Implementation Plan

> **For agentic workers:** Execute task-by-task with verification checkpoints.

**Goal:** Convert the latest upstream source into a standalone OpsPilot distribution and validate the `v1.0.0` release on the test machine.

**Architecture:** Apply a repository-wide identifier migration, then repair structured references in Go, shell, Compose, Docker, Helm, CI, and documentation. Publish all artifacts from the migrated repository and deploy the released package on a fresh test environment.

**Tech Stack:** Go 1.25, React/Vite, Docker Compose, GitHub Actions, CNB registry/release API, Bash installers.

---

### Task 1: Baseline and rename map

- [ ] Record current Go/tool versions and inventory legacy brand references.
- [ ] Define replacement rules for product identifiers, paths, environment variables, image repositories, and third-party names.
- [ ] Commit the plan and baseline notes.

### Task 2: Go module and source identifiers

- [ ] Rename command directories and source file references to OpsPilot names.
- [ ] Change `go.mod` module path and all internal imports.
- [ ] Rename Go constants, defaults, and user-facing server identifiers.
- [ ] Run `gofmt`, `go test ./...`, and build manager/Edge binaries.

### Task 3: Runtime and deployment identifiers

- [ ] Rename legacy environment variables to `OPSPILOT_*` in source, examples, Compose, scripts, and tests.
- [ ] Rename install/data/log/cache paths, Docker services, volumes, certificates, and systemd units.
- [ ] Rename Dockerfiles, Helm chart metadata, dashboards, and generated package paths.
- [ ] Validate shell syntax, Compose config, and package targets.

### Task 4: Release automation

- [ ] Set CNB image and Edge release targets to `zara1024/opspilot` and `zara1024/opspilot-edge`.
- [ ] Update GitHub workflows, Make targets, release manifests, download URLs, and archive names.
- [ ] Set `VERSION` to `v1.0.0` and verify release workflow prerequisites.

### Task 5: Verification and release

- [ ] Run old-brand scan, Go tests/builds, frontend build, shell/Compose checks, and package tests.
- [ ] Push the migrated branch and `v1.0.0` tag to the OpsPilot GitHub repository.
- [ ] Verify CNB/GitHub release artifacts and checksums without exposing credentials.

### Task 6: Test-machine deployment

- [ ] Check Go, Docker, and Compose versions on `43.106.103.99`.
- [ ] Deploy the published `v1.0.0` package as a fresh OpsPilot installation.
- [ ] Verify health endpoints, container state, image names, and Edge assets; record failures with logs.
