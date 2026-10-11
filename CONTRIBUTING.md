# Contributing

This guide applies to people and coding agents. Submit changes to
[`ohne-b/OhneGuessr`](https://github.com/ohne-b/OhneGuessr), whose default branch is
`main`. Preserve the [license](LICENSE.md) and third-party notices.

Keep changes focused on the stated problem. Reuse existing helpers and controls;
discuss substantial features or refactoring first unless already authorized.
Contributor policy lives here; `AGENTS.md` is the agent entry point. Keep technical
contracts in [Architecture and test ownership](docs/architecture.md) and the
[plugin guide](plugins/README.md). Update those when their contracts change. README
covers installation, essential usage, updates and troubleshooting. Extend existing
documentation instead of adding duplicate policies or planning documents.

## Preserve the workspace and privacy

Inspect `git status` and the diff before editing. Preserve unrelated changes,
credentials, settings, user data, logs and backups. If an unfinished operation or
unrelated work prevents safe edits, use a separate worktree; do not reset, stash or
resolve someone else's work without authorization. Stage only intended files.

Before committing, inspect the configured Git author and committer identity locally.
For maintainer work, verify the public `ohne-b` identity and configured noreply email
against GitHub account information or existing public commits associated with that
account; never guess the address. Keep private names, emails, credentials and local
configuration out of commits, artifacts, screenshots, logs and PR evidence. Never
copy a local CLI credential into an automation secret.

Automated tests use temporary storage, synthetic maps and mocked transports, such
as `t.TempDir()`, `httptest` and the existing Vitest mocks. Never point them at real
user profiles, API keys, live map/sync services or running installations.
Implementation, merging, releasing and deploying require separate authorization;
permission for one does not grant the others. Never rewrite published history or
tags without explicit authorization.

## Development

Use Go 1.26 and Node.js 24 (the CI versions). Other Node versions must satisfy
`frontend/package.json`. Wails v3 is pinned as a Go tool in `go.mod`; no global CLI
installation is needed. Run commands from the repository root:

```powershell
npm --prefix frontend ci
go tool wails3 dev
```

The development app uses the normal application data directory, including
`%LOCALAPPDATA%\OhneGuessr` on Windows. For manual checks with disposable data,
launch a built executable with `--data-dir` pointing to a separate test directory.
This does not make network features offline or bypass the single-instance app lock.

```powershell
go tool wails3 build
# Windows installer: install NSIS first and put makensis on PATH.
go tool wails3 task windows:package VERSION=0.0.0
```

Build output goes to `bin/`. Wails tasks generate bindings and build the frontend;
they currently use `npm install`, so inspect any lockfile changes. For frontend-only
work use `npm --prefix frontend run dev` or `npm --prefix frontend run build`.
The browser dev server alone does not supply native Wails services. Build creates
the ignored `frontend/dist/`; build it before invoking Go commands that include the
root package, because `main.go` embeds it.

Windows needs WebView2 to run the app and NSIS only for installer packaging.
Linux builds need a C compiler, `pkg-config`, GTK 4 and WebKitGTK 6 development
packages (CI installs `build-essential libgtk-4-dev libwebkitgtk-6.0-dev pkg-config`).
macOS builds use Xcode command-line tools; the existing
`go tool wails3 task darwin:package:universal VERSION=0.0.0` task builds both architectures.

## Branches, commits and PRs

Start a descriptive `feat/` or `fix/` branch from current canonical `main`. For an
`origin` pointing to this repository:

```powershell
git fetch origin
git switch -c fix/short-description origin/main
# Before final validation/review, and again before merge if main advances:
git fetch origin
git merge origin/main
```

For forks, fetch/integrate the canonical `upstream/main` and push to your fork.
Resolve conflicts deliberately and rerun affected checks after integration. Use
focused conventional commits and conventional PR titles, such as
`fix(settings): preserve unsaved changes`. Give substantial independent changes
meaningful commit boundaries; do not artificially split small changes. Keep branch
names, commit messages, PRs and documentation free of assistant branding and phrases
such as "as requested".

Submit through a PR against canonical `main`, never directly to it. Keep incomplete
PRs in draft, including those awaiting applicable checks or independent review.
When merging is explicitly authorized, integrate `main` again if it advanced and
use a merge commit with a conventional subject and the PR number, for example
`fix(settings): preserve unsaved changes (#123)`. Never use GitHub's default
"Merge pull request..." message, squash or rebase in place of that merge commit.

## Validation

Run focused local checks for the changed behavior. Add meaningful regression tests
for fixes and behavior changes, including relevant failure cases. Go tests live
beside their packages; Vitest covers `frontend/test/*.test.ts` and
`plugins/<id>/*.test.js`. For example:

```powershell
go test ./internal/backend -run TestHTTPMapsAndInternalRouting
npm --prefix frontend test -- test/game-deck.test.ts
```

For map sampling measurements, use
`go test ./internal/backend -run '^$' -bench BenchmarkSampleMapLocations -benchmem`.

Use the relevant commands below; successful CI on the final revision supplies the
full baseline, so do not repeat the whole suite locally without a reason.

| Change | Applicable checks |
| --- | --- |
| Go behavior | Focused package tests; `go vet ./...` and `go test ./...`. Concurrency changes also need `go test -race ./...` with CGO and a C compiler. |
| Frontend or built-in features | `npm --prefix frontend test`; `npm --prefix frontend run build` includes Svelte/TypeScript checks and Vite. Check affected UI in the native app, including narrow layouts, keyboard access and focus; record what was actually exercised. |
| Downloadable plugins | `npm --prefix plugins run build` after source/manifest edits; commit generated entry points and registry. `npm --prefix plugins run check` verifies types and generated files; the frontend test command includes plugin tests. |
| Wails service/API changes | `go tool wails3 generate bindings -ts -i -noevents ./...`; commit generated bindings, never hand-edit them. Rebuild the frontend and backend together. |
| Build, packaging or updater changes | Relevant platform build/package checks and isolated failure cases. Use native runners; frontend tests cannot validate installers or native updates. |
| Documentation only | `git diff --check` plus the branch diff against canonical main; verify relative links, readable Markdown, commands against their definitions, and factual claims. No local application build is needed. |

[Check](.github/workflows/check.yml) runs on every push and PR, including docs-only
changes. Its Linux `check` job installs frontend dependencies, runs frontend/plugin
tests, frontend build and plugin checks, regenerates Wails bindings and rejects
modified or untracked bindings, checks tracked Go files with `gofmt -l`, then runs
`go vet ./...` and `go test -race ./...`. These checks must pass on the final PR
revision. Preserve them; the docs-only local path does not disable CI. Its
`windows-package` job runs only on manual dispatch, builds a temporary portable EXE
and NSIS installer, runs Go tests, and retains the packages for seven days. It is
not an automatic PR check or a release.

Run `git diff --check` for every change. Record exact commands, results, CI links
and limitations; never call skipped, pending or unrun checks successful. After
review fixes or main integration, rerun affected checks and update the evidence.

## Independent review and PR evidence

Every PR, including documentation-only changes, needs independent adversarial
review by a human or a separate read-only reviewer who did not author it. The
author's reread does not count. Give the reviewer the goal, acceptance criteria,
final diff/commit, contributor instructions and actual test evidence. Ask them to
look for incorrect claims, regressions, privacy/security issues, missed failure
cases, unnecessary scope and missing coverage. Record their identity or role,
reviewed commit, findings and resolutions; justify rejected findings with evidence.
Have substantive fixes and later relevant changes rechecked.

Use the [PR template](.github/pull_request_template.md) to record the incorporated
canonical main SHA, tested head SHA, check results, independent review and remaining
limitations. Update the description to match the final change, not its development
history. Keep the PR in draft while review is unavailable or blocking findings or
checks remain unresolved. Separate reviewer evidence does not replace any required
GitHub approvals. Check current main and all applicable requirements again before
an authorized merge.

## Releases

Release work needs explicit authorization. Preserve the existing manual
[Release workflow](.github/workflows/release.yml):

1. Prepare and review changes through the normal PR process, including concise
   `CHANGELOG.md` notes with relevant PR numbers. Stable release notes cover changes
   since the previous stable release.
2. After an authorized merge and successful checks, create a `vMAJOR.MINOR.PATCH`
   tag at the approved commit and a GitHub draft release for it. Dispatch **Release**
   with that existing tag. The tag supplies the version; the workflow
   passes it to Wails tasks and sets Windows metadata during the build. Do not bump
   placeholder package/build versions merely to prepare a release.
3. The workflow builds Windows x64 portable/Setup EXEs, Linux x64 AppImage/DEB and
   universal macOS DMG/updater archive from the tag. It checks Linux installation
   and launch behavior, validates packages, signs updater digests, and uploads
   those six files plus `latest.json` and `SHA256SUMS.txt` to the existing draft.
4. Verify the actual uploaded files and checksums before publishing within the
   explicit release authorization. The workflow neither creates nor publishes the
   draft release.
   Ordinary code merges do not run it. The downloadable `plugins/` catalog is
   served directly from GitHub `main` to released clients, so catalog changes have
   a separate compatibility impact even without an application release.

Updater signing uses the `OHNEGUESSR_UPDATE_PRIVATE_KEY` Actions secret only in
the release upload job. It must match the public key in the workflow and
`internal/updates/service.go`. Keep signing secrets out of PR checks, never silently
rotate the key, and preserve the manifest's legacy setup/portable fields. Updater
Ed25519 signatures are separate from OS signing: Windows releases are unsigned and
macOS uses ad-hoc signing, not Developer ID/notarization. The existing architecture
guide owns release script inputs and outputs; do not add another release system.
