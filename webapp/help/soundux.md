# Switch from Soundux

Resound does what Soundux did on Windows: it plays your sounds into games and voice chat through VB-Cable, holds your push-to-talk key and mutes your mic while sounds play. If Soundux is set up on your PC, the [guided setup](guided-setup.md) offers to carry its settings over, so you don't have to add your sounds and keys again.

## Import your settings

1. On the setup's welcome page, the **Coming from Soundux?** card appears above the setup's buttons. Click **Import from Soundux…** in it.
2. Each kind of setting found in Soundux has a switch. Turn off what you'd rather set up fresh.
3. Click **Import and continue**. The setup goes on with its usual steps, already filled in, so you can check everything: your folders in the first step, your microphone setting in the second, your push-to-talk key in the third.

The welcome page only offers the import when Soundux's settings are found (`%APPDATA%\Soundux\config.json`) and hold something Resound can use.

:::tip Quit Soundux first
If Soundux is still running, both apps press your push-to-talk key and react to your keys. Quit it from its tray icon. The import page warns you while it runs.
:::

## What carries over

| In Soundux | In Resound |
|---|---|
| Tabs | Sound folders. Each folder becomes a tab, and so does each of its subfolders with sounds ([Sound folders and tabs](sound-library.md)) |
| Tabs sorted by name | Sorted by name on your phone too |
| Favorites | Favorites, also in the [in-game overlay](overlay.md) |
| A sound's own volumes | The sound's own volumes, for you and for others |
| The two volume sliders | **You hear** and **Others hear**, converted so they sound as loud as before |
| Push-to-talk keys | The push-to-talk key |
| Stop hotkey | A [stop key](push-to-talk.md#stop-keys) |
| Allow overlapping | **Allow sounds to overlap** |
| Mute during playback | **Mute my microphone while sounds play**, with **Minimum sound length** set to **Always**, as Soundux muted it for every sound |

Resound [evens out the loudness](volume.md) of your sounds. If they sound quieter or louder than in Soundux, adjust the sliders on the home page.

## What doesn't carry over

- **Audio devices.** The setup's audio step chooses them and sets up VB-Cable. Soundux didn't save a microphone either: like Resound, it used your default one.
- **Hotkeys of single sounds.** Resound plays sounds by key from the [in-game overlay](overlay.md): mark the sounds as favorites, then one key opens the overlay and plays them.
- **Sounds and folders that are gone.** Folders that aren't found (a drive that isn't connected, a moved folder) are listed and skipped. So are the settings of sounds whose files were renamed or deleted since.
- **Tabs sorted by date, oldest first.** Resound lists sounds newest first, as Soundux did by default.
- **Soundux's look**: theme, view mode, window size, the tray option.

## Your settings are safe

- The import only reads Soundux's file. Soundux keeps working as before, and you can [uninstall it](#uninstall-soundux) whenever you like.
- Settings you have in Resound already are kept. Favorites are added, a sound's own volume set in Resound stays, and keys are added next to yours. A key that already does something else in Resound is left out, and the first step says so.
- If Resound was set up before, it [backs up](backup.md) your current settings first (`Resound-backup-…-before-soundux.zip`), so you can go back.

To import later: Settings → **Run the guided setup again**, then **Import from Soundux…** on its welcome page.

## Uninstall Soundux

Once you're happy with Resound, uninstall Soundux in Windows' Settings → Apps → Installed apps. Leave VB-Cable installed: Resound uses it too.
