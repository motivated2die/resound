# Contributing to Resound

Thanks for helping! Bug reports, ideas and pull requests are all welcome.

## Reporting bugs

Open an issue with your Resound version (Settings → App & startup), your Windows version, what you did and what happened. The log helps a lot: Settings → Diagnostics → Open log (`%LOCALAPPDATA%\Resound\debug.log`). Security problems go through [SECURITY.md](SECURITY.md) instead.

## Making changes

1. Read [docs/BUILDING.md](docs/BUILDING.md) to set up and build, and [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for how the parts fit together.
2. Create a branch from `main` and make your change. Match the style of the code around it.
3. Before you push:
   ```powershell
   gofmt -l .        # must print nothing (gofmt -w <file> fixes a file)
   go vet ./...
   go test ./...
   ```
   CI runs the same checks on every pull request.
4. Test the change in the running app; `app`, `install`, `ui` painting and the WASAPI code have no automated tests.
5. Open a pull request against `main` that describes what changed and how you tested it.

**Commit messages** start with a type: `feat:` (new feature), `fix:` (bug fix), `ui:` (look and feel), `docs:`, `chore:` (build, dependencies, cleanup). They become the release notes, so write them for users: `fix: phone remote reconnects after the PC wakes up`.

**Dependencies.** The project is pure Go without cgo; please keep it that way. New Go modules and front-end libraries must have a license compatible with GPL-3.0 (MIT, BSD, Apache-2.0, …) and are listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). Front-end libraries are vendored under `webapp/src/vendor/` with their license file, not loaded from a CDN.

Releases are made by the maintainers; see [docs/RELEASING.md](docs/RELEASING.md).

## License

By contributing, you agree that your contributions are licensed under the [GNU General Public License v3.0](LICENSE), like the rest of the project.
