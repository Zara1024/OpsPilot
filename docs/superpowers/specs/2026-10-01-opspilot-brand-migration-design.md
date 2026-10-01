# OpsPilot Brand Migration Design

## Goal

Create a standalone OpsPilot distribution from the latest upstream source. This is a hard rename: runtime, build, deployment, packaging, documentation, and release identifiers use `opspilot`; legacy identifiers are not accepted at runtime.

## Scope

- Change the Go module path to `github.com/Zara1024/OpsPilot` and update internal imports.
- Rename command directories and binary outputs to `opspilot`, `opspilot-edge`, and `opspilot-edge-supervisor`.
- Rename legacy environment variables to `OPSPILOT_*` and update all readers, examples, Compose files, installers, and tests.
- Rename installation, data, log, Compose project, container, volume, certificate, and cache defaults to `opspilot`.
- Publish manager, web, and Kubernetes Edge images under `docker.cnb.cool/zara1024/opspilot` and publish Edge release attachments to `zara1024/opspilot-edge`.
- Rename package archives, release metadata, download URLs, Helm references, dashboard identifiers, and user-facing documentation.
- Remove old compatibility branches and old-brand defaults rather than adding aliases.

## Implementation Rules

Use structured replacements where identifiers affect Go imports, shell variables, YAML keys, Docker service names, or file paths. Rename filesystem paths before updating references. Keep third-party names such as `singchia/frontier` unchanged. Do not write credentials or tokens to tracked files.

## Validation

1. Scan tracked files for legacy brand references, allowing only an explicit migration note if one is required.
2. Record the Go version declared by `go.mod` and the local toolchain version. On the test machine, record `go version` before building; if it is older than the module requirement, use a compatible toolchain there or stop with an explicit prerequisite error. Do not silently lower the module's Go requirement.
3. Run `gofmt` and `go test ./...` with the selected compatible toolchain.
4. Run frontend dependency checks and production build using the existing project scripts.
5. Validate Compose and shell syntax, Make targets, and GitHub workflow references.
6. Build manager and Edge binaries and verify their names and embedded module imports.
7. Review release configuration for the OpsPilot CNB repositories and confirm no secret is present in the worktree.
8. Set `VERSION` to `v1.0.0`, create the `v1.0.0` tag on the migrated commit, and push the branch and tag to the OpsPilot GitHub repository to trigger automated release publishing.
9. Wait for the release workflow to publish the CNB manager/web images, `zara1024/opspilot-edge` attachments, and versioned installation package. Verify the published checksums and package metadata before deployment.
10. Deploy the published `v1.0.0` package on `43.106.103.99` as a fresh OpsPilot installation, then verify container health, `/healthz`, `/readyz`, and the OpsPilot Edge artifact path.

## Test Machine Toolchain Check

The deployment verification target is `root@43.106.103.99`. The test procedure begins with `go version`, `docker version`, and `docker compose version`, then checks the repository's declared Go requirement before any build or deployment command. Credentials are supplied out of band for the temporary session and are never committed, echoed, or stored in shell scripts.

## Deployment Assumption

This release is intended for a fresh OpsPilot installation. Existing legacy installations, volumes, environment files, and containers are outside the compatibility contract and require manual migration.
