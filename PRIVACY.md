# Privacy policy

*Last updated: 2026-10-12*

Resound is a soundboard that runs on your Windows PC. It has no user accounts, no analytics or telemetry, no ads and no servers of its own. The project and its maintainers receive no data from it.

## What stays on your PC

Resound stores its settings and data only on your PC, in `%LOCALAPPDATA%\Resound`:

- settings (`config.json`): chosen audio devices, volumes, push-to-talk keys, sound folders;
- the sound library index (`library.json`): file names, paths, lengths and fingerprints of your sounds and their settings;
- phone pairing (`remote.json`): the remote PIN and hashes of paired phones' session tokens;
- backups you make, and a log file (`debug.log`) for troubleshooting.

None of this is sent anywhere. Uninstalling Resound deletes this folder.

## Network connections

**Your phone and your PC.** Resound runs a small web server on your PC (port 8080 by default) so your phone can control it over your local network. Only browsers that paired with your PIN or QR code can use it. The connection is plain HTTP inside your network and never goes through the internet or a third party. Everything the remote's page needs (icons, the emoji picker and its emoji list) comes from Resound itself, and the page tells the browser not to load anything from elsewhere, so your phone contacts no other server while you use it.

**Connections Resound makes itself:**

| When | To | What is sent |
|---|---|---|
| Shortly after Resound starts, then every 12 hours (turn off under Settings → App & startup → "Check for updates automatically") | `api.github.com` (GitHub) | A request for this project's latest release, to see whether a newer version exists. It carries Resound's version in the user agent and nothing else. GitHub sees your IP address, see [GitHub's privacy statement](https://docs.github.com/site-policy/privacy-policies/github-general-privacy-statement) |
| You choose "Update now" | `github.com` and GitHub's download servers (`*.githubusercontent.com`) | A normal download request for the new release's `Resound.exe` and `SHA256SUMS.txt` |
| You choose "Install VB-Cable" | `download.vb-audio.com` (VB-Audio Software) | A normal download request for the VB-Cable driver package. As with any download, VB-Audio's server sees your IP address. See [VB-Audio's website](https://vb-audio.com) for their policies |

Links in Resound (VB-Audio's website and donation page, this project's pages) open in your browser only when you click them.

## Changes to your system

Resound changes Windows settings only when you ask it to, and tells you before it does:

- installing itself for your user (Installed apps entry, Start Menu shortcut), and starting with Windows only if you turn that on;
- updating itself when you choose "Update now";
- installing VB-Cable (with Windows' permission prompt), and restoring your default audio devices if VB-Cable's setup changed them;
- turning on "Listen to this device" for your microphone so it reaches the virtual cable (with Windows' permission prompt);
- a Windows Firewall rule that lets your phone connect (with Windows' permission prompt);
- muting your microphone and pressing your push-to-talk key while a sound plays, if you turn these on.

## Contact

Questions about privacy: open an issue at [github.com/motivated2die/resound/issues](https://github.com/motivated2die/resound/issues). If this policy changes, the new version is published in this file, and its history is visible in the repository.
