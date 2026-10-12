# Security policy

## Supported versions

Only the [latest release](https://github.com/motivated2die/resound/releases/latest) receives fixes. Please check that a problem still exists there before reporting it.

## Reporting a vulnerability

Please **don't** open a public issue for security problems. Report them privately through GitHub instead: **Security** tab → **Report a vulnerability** ([direct link](https://github.com/motivated2die/resound/security/advisories/new)).

Helpful details: the Resound version, what an attacker can do and from where (the same PC, the local network), and steps to reproduce it.

You can expect a first answer within a week. Once a fix is released, the advisory is published, crediting you if you like.

## Scope

In scope, for example: the phone remote's API and pairing (PIN, QR codes, sessions), the elevated helpers (`--apply-routing`, `--allow-firewall`), the installer and updater, and the VB-Cable download and signature check.

Known limitation, not a vulnerability: the phone remote uses plain HTTP on the local network (see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md#known-issues-and-gaps)).
