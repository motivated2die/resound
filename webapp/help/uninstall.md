# Uninstall Resound

## Before you start

Uninstalling deletes all of Resound's settings, **including the backups in its backup folder**. If you might come back, save a backup somewhere else first: Settings → Backup & restore → **Save as…**. See [Backup and restore](backup.md).

## Uninstall

Either way works:

- **Windows:** Settings → Apps → **Installed apps** → Resound → ⋯ → **Uninstall**.
- **Resound:** Settings → App & startup → **Uninstall Resound…**.

Confirm with **Uninstall**. Resound quits and removes itself; a message says when it's done.

## What's removed

- the program in `%LOCALAPPDATA%\Programs\Resound`, its Start Menu entry and autostart;
- everything in `%LOCALAPPDATA%\Resound`: settings, the sound index (favorites, colors, layouts), paired phones, backups stored there and the log.

## What stays

- **Your sound files.** Resound never deletes them.
- **VB-Cable**, **Listen to this device** on your microphone and Resound's **firewall rule**. Remove them by hand if you like: [Remove VB-Cable and Windows changes](remove-vb-cable.md).
- **The phone app.** Remove it like any app:
  - Android: long-press the Resound icon → **Uninstall** (or drag it to Uninstall).
  - iPhone and iPad: long-press the icon → **Remove App** → **Delete from Home Screen**.
  - To remove its stored pairing too, clear the site data for getresound.app in the browser's settings.

Afterwards, switch the microphone in Discord and your games back from CABLE Output to your headset, or uninstall VB-Cable too.
