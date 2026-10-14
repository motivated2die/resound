# Releasing Resound

How versions, tags and releases work, step by step. Building in general is in [BUILDING.md](BUILDING.md).

## The idea in one picture

```
 feature branch ──PR──▶ main ──(test locally)──▶ git tag v0.7.0 ──push──▶ GitHub Actions
                         │                                                 │ build: vet, test, exe
                       CI checks                                           │ sign: (later, SignPath)
                       every change                                        ▼
                                                              DRAFT release with Resound.exe
                                                                           │ you review it
                                                                           ▼
                                                              Published release (users see it)
```

- **`main`** always holds the latest finished work. Every push and pull request to it is checked by CI ([ci.yml](../.github/workflows/ci.yml)): formatting, vet, tests and a build.
- **Nothing is released automatically from `main`.** You decide which commit becomes a release by putting a **version tag** on it.
- **A tag** is a permanent name for one commit, like a bookmark: `v0.7.0` → commit `3f37898`. Unlike a branch, it never moves. Pushing a tag that looks like a version starts the release workflow ([release.yml](../.github/workflows/release.yml)).
- The workflow creates a **draft** release. Drafts are visible only to you and other maintainers. The release becomes public only when you click **Publish**.

## Version numbers

Resound uses [semantic versioning](https://semver.org): `MAJOR.MINOR.PATCH`, and the tag is the version with a `v` in front.

| Change since the last release | Bump | Example |
|---|---|---|
| Only bug fixes | PATCH | 0.7.0 → 0.7.1 |
| New features (settings and data stay compatible) | MINOR | 0.7.1 → 0.8.0 |
| Incompatible changes; or "1.0, it's stable now" | MAJOR | 0.9.3 → 1.0.0 |

- **Pre-releases** add a suffix: `v0.8.0-rc.1`, `v0.8.0-rc.2`, then `v0.8.0`. `rc` means release candidate; `-beta.1` works too. They become GitHub pre-releases, which the "latest release" link and the in-app update skip, so you can hand them to testers safely.
- **A version is used once.** Once a release with a version is published, never publish different files under that number again; release the next PATCH instead.
- The version in the source (`app.Version = "0.13.0-dev"` in `internal/app/app.go`, and `winres/winres.json`) is only the default for development builds. Release builds take the version from the tag. You don't edit any file to make a release.

## One-time setup on GitHub

Do these once per repository (and again after moving to a new repository).

1. **Actions** (Settings → Actions → General): allow GitHub Actions. Workflow permissions can stay at the default "Read repository contents"; the release workflow asks for write access itself where it needs it.
2. **Protect `main`** (Settings → Rules → Rulesets → New branch ruleset):
   - Target: the default branch. Enforcement: Active.
   - Rules: *Restrict deletions*, *Block force pushes*, *Require status checks to pass* → add `test` (the CI job; it appears in the list after CI has run once).
   - Optional, recommended once others contribute: *Require a pull request before merging*. You can merge your own pull requests without a review.
3. **Protect version tags** (New tag ruleset): target pattern `v*`, rules *Restrict updates* and *Restrict deletions*. A published version then can't be moved or removed by accident. Repository admins can still be allowed to bypass it (Bypass list → Repository admin), for cleaning up a failed attempt (see [Fixing mistakes](#fixing-mistakes)).
4. **Two-factor authentication** on every maintainer's GitHub account (also required for code signing later).
5. **When the repository is public:** Settings → Security → *Private vulnerability reporting* → Enable ([SECURITY.md](../SECURITY.md) points there).

**Private vs. public repositories.** Both work the same, with two differences: in a private repository, Actions minutes are limited (Windows runners count double against the free allowance), and the build-provenance attestation is skipped because GitHub only offers it for free on public repositories. Releases of a private repository are only visible to people with access.

## Making a release

Example: releasing 0.7.0. Commands are for PowerShell in the repository folder.

### 1. Get `main` ready

Merge everything that belongs in the release into `main` and wait for CI to pass (the green check next to the latest commit on GitHub). Then update your local copy:

```powershell
git switch main
git pull
git status            # must say "nothing to commit, working tree clean"
```

### 2. Build and test locally

```powershell
.\scripts\build-release.ps1 -Version 0.7.0
```

This runs vet and the tests, and puts `Resound.exe` and `SHA256SUMS.txt` into `Desktop\Resound-0.7.0`. If it warns about uncommitted changes, you're not testing exactly what you'll release. Commit or discard them first.

Go through the [release checklist](#release-checklist) with this exe. Found a problem? Fix it on `main` and start again at step 1. Nothing has been published yet.

### 3. Tag the commit and push the tag

```powershell
git log -1 --oneline                          # the commit you just tested
git tag -a v0.7.0 -m "Resound 0.7.0"        # tag it (annotated tag with a message)
git push origin v0.7.0                        # pushing the tag starts the release workflow
```

`git push` without the tag name doesn't push tags, so you push it by name. The workflow refuses tags whose commit is not on `main`, and tags that don't look like `v1.2.3` or `v1.2.3-rc.1`.

### 4. Watch the workflow

On GitHub: **Actions** → **Release** → the run named after your tag. It takes a few minutes:

| Job | What it does |
|---|---|
| `build` | Checks the tag, installs the pinned Go and go-winres, runs the same `scripts\build-release.ps1` as you did in step 2, and uploads the unsigned exe as a workflow artifact |
| `sign` | Skipped until code signing is set up ([below](#code-signing-later)) |
| `release` | Computes `SHA256SUMS.txt`, attests the build (public repositories), writes the release notes from the commit messages since the last release, and creates the **draft** release |

If a job fails, open it to read the log. See [Fixing mistakes](#fixing-mistakes).

### 5. Review and publish the draft

**Releases** (right side of the repository's main page) → the draft "Resound 0.7.0":

1. Read the notes. They list every commit since the previous release, so this is the place to tidy them up, e.g. add a short summary at the top.
2. Optionally download `Resound.exe`, run it on a test PC, and check that its hash matches the notes.
3. **Publish release.**

Done: the release is public, and `https://github.com/motivated2die/resound/releases/latest` points at it (for pre-releases: not). Installed copies show the update dot at their next start or within 12 hours, and update with one click. **Publishing is what ships an update to every user**, so only publish an exe you have tested.

### Release candidates

To have a version tested before it's final, tag `v0.7.0-rc.1` in step 3 instead. The draft is marked *pre-release*; publish it and share its link with testers. Fixes go to `main` and become `v0.7.0-rc.2`. When everything is fine, tag the last tested commit `v0.7.0` (a commit can carry several tags). The notes of the final release cover everything since the previous final release.

## Fixing mistakes

| Situation | What to do |
|---|---|
| The workflow failed (e.g. a test), nothing was published | Fix it on `main`. Delete the tag (below), delete the draft release if one exists, then tag the fixed commit again with the **same** version |
| The workflow failed for a passing reason (GitHub outage) | Actions → the failed run → **Re-run failed jobs**. A re-run replaces the files of the draft |
| You tagged the wrong commit, nothing published yet | Delete the tag (below) and tag the right commit |
| A **published** release has a bug | Don't change or delete its tag. Fix it on `main` and release the next PATCH version (0.7.1). If the bug is serious, edit the bad release and mark it as pre-release, so "latest" falls back to the previous one |

Deleting a tag, locally and on GitHub (needs the bypass from the tag ruleset):

```powershell
git tag -d v0.7.0
git push origin --delete v0.7.0
```

Always create tags with `git tag` as above, not on GitHub's "Draft a new release" page: a release created there gets no exe, because the release workflow makes its own.

## Verifying a release

Anyone can check that a downloaded `Resound.exe` is the one published:

```powershell
(Get-FileHash .\Resound.exe -Algorithm SHA256).Hash        # compare with the release notes / SHA256SUMS.txt
gh attestation verify .\Resound.exe --repo motivated2die/resound   # public repositories: built by this repo's workflow
```

The attestation proves the exe was built by the release workflow from the tagged commit, not uploaded by hand.

Your local build and the workflow's build use the same script and the same Go release (`toolchain` in `go.mod`), but don't expect identical hashes: the workflow's exe is the one that's published.

## Code signing (later)

The release workflow already contains a `sign` job for [SignPath Foundation](https://signpath.org) code signing. It is skipped while the repository variable `SIGNPATH_ORGANIZATION_ID` doesn't exist. Once the project is accepted:

1. **In SignPath** (with the account SignPath creates):
   - Add the predefined *GitHub.com* trusted build system and link it to the project. Install the SignPath GitHub App on the repository if asked.
   - Create an artifact configuration. The workflow uploads the exe as a zipped artifact, so its root is a zip file:
     ```xml
     <?xml version="1.0" encoding="utf-8"?>
     <artifact-configuration xmlns="http://signpath.io/artifact-configuration/v1">
       <zip-file>
         <pe-file path="Resound.exe" product-name="Resound">
           <authenticode-sign />
         </pe-file>
       </zip-file>
     </artifact-configuration>
     ```
   - Note the organization ID, the project slug and the signing policy slug (usually `release-signing`). Create an API token for a CI user with submitter rights.
2. **In GitHub** (Settings → Secrets and variables → Actions):
   - Secret `SIGNPATH_API_TOKEN`: the API token.
   - Variables `SIGNPATH_ORGANIZATION_ID`, `SIGNPATH_PROJECT_SLUG`, `SIGNPATH_SIGNING_POLICY_SLUG`.
3. **README**: replace the "not code-signed yet" sentence under *Code signing policy* with SignPath's required wording: "Free code signing provided by [SignPath.io](https://about.signpath.io), certificate by [SignPath Foundation](https://signpath.org)". Keep the team roles and the privacy link current.

From then on every release waits in the `sign` job until an approver approves the signing request in SignPath, and the draft gets the signed exe. The release notes drop the SmartScreen hint automatically.

## Release checklist

Before tagging (step 2), with the locally built exe, ideally on a clean user account or VM:

1. After a logo change: `go run ./scripts/icons` was run and committed (app icon and phone icons).
2. `.\scripts\build-release.ps1 -Version X.Y.Z` passes without warnings.
3. Install: the welcome notice appears; Start Menu and Installed apps show the new version.
4. The guided setup: all five steps, and skipping them.
5. Phone pairing by QR code; a sound plays to both outputs; push-to-talk works.
6. Update over the previous release: settings, library and pairing are kept, and "updated" is shown. Also the in-app update from the previous version, with `cmd/devupdate` offering this exe ([BUILDING.md](BUILDING.md#testing-updates-locally)).
7. Uninstall.

After publishing:

8. The release page shows `Resound.exe` and `SHA256SUMS.txt`, and `releases/latest` points at it (not for pre-releases).
9. An installed previous version shows the update dot (Settings → App & startup → "Check now") and updates to it.
