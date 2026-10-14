# Privacy policy

*Last updated: 2026-10-13*

Resound is a soundboard that runs on your Windows PC. It has no user accounts, no analytics or telemetry, no ads and no servers of its own. The project and its maintainers receive no data from it.

## What stays on your PC

Resound stores its settings and data only on your PC, in `%LOCALAPPDATA%\Resound`:

- settings (`config.json`): chosen audio devices, volumes, push-to-talk keys, sound folders;
- the sound library index (`library.json`): file names, paths, lengths and fingerprints of your sounds and their settings;
- phone pairing (`remote.json`): the IDs of paired phones and the keys the connection uses;
- backups you make, and a log file (`debug.log`) for troubleshooting.

None of this is sent anywhere. Uninstalling Resound deletes this folder.

## Network connections

**Your phone and your PC.** You control Resound with the Resound app, a web app your phone loads from a static website (`getresound.app`, hosted on Cloudflare Pages) and can install. Like any website, the host sees your phone's IP address and browser when the app's files are downloaded; an installed app starts from your phone's cache and checks the host for a new version when you open it. Everything the app needs (icons, the emoji picker and its emoji list) comes from that host, and the app tells the browser not to load anything from elsewhere. The same site has an information page with links to this project on GitHub (the download, the source, this policy); those load only when you follow them.

The pairing details in the PC's QR code (your PC's local addresses, a one-time code, the PC's name) come after a `#` in the link, which browsers never send to the host. The app then talks to your PC directly over your local network (UDP port 8080 by default), encrypted (WebRTC); nothing about your sounds or your use passes through the host or any other server. Only devices paired with a QR code can connect. The app stores the list of paired PCs and their keys on your phone only.

**Connections Resound makes itself:**

| When | To | What is sent |
|---|---|---|
| Shortly after Resound starts, then every 12 hours (turn off under Settings → App & startup → "Check for updates automatically") | `api.github.com` (GitHub) | A request for this project's latest release, to see whether a newer version exists. It carries Resound's version in the user agent and nothing else. GitHub sees your IP address, see [GitHub's privacy statement](https://docs.github.com/site-policy/privacy-policies/github-general-privacy-statement) |
| You choose "Update now" | `github.com` and GitHub's download servers (`*.githubusercontent.com`) | A normal download request for the new release's `Resound.exe` and `SHA256SUMS.txt` |
| You drag a sound or a link from a website (for example Myinstants or Sound Buttons World) onto a folder in Resound | That website (for example `www.myinstants.com` or `soundbuttonsworld.com`) | A normal request for the dropped page and its sound file, as your browser makes when you download it. Resound saves the sound into the folder you dropped it on and sends nothing else. Links to your own PC or local network are never requested |
| You choose "Install VB-Cable" | `download.vb-audio.com` (VB-Audio Software) | A normal download request for the VB-Cable driver package. As with any download, VB-Audio's server sees your IP address. See [VB-Audio's website](https://vb-audio.com) for their policies |

Links in Resound (VB-Audio's website and donation page, this project's pages) open in your browser only when you click them.

## Changes to your system

Resound changes Windows settings only when you ask it to, and tells you before it does:

- installing itself for your user (Installed apps entry, Start Menu shortcut), and starting with Windows only if you turn that on;
- updating itself when you choose "Update now";
- installing VB-Cable (with Windows' permission prompt), and restoring your default audio devices if VB-Cable's setup changed them;
- turning on "Listen to this device" for your microphone so it reaches the virtual cable (with Windows' permission prompt);
- a Windows Firewall rule that lets your phone connect (with Windows' permission prompt; it covers the app's UDP connection too);
- muting your microphone and pressing your push-to-talk key while a sound plays, if you turn these on;
- watching the keyboard for your pause and stop keys, only while you have set at least one. Resound looks only for those keys; it doesn't record, store or send anything you type.

## Contact

Questions about privacy: open an issue at [github.com/motivated2die/resound/issues](https://github.com/motivated2die/resound/issues). If this policy changes, the new version is published in this file, and its history is visible in the repository.
