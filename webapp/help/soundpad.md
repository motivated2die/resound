# Switch from Soundpad

If Soundpad is set up on your PC, the [guided setup](guided-setup.md) offers to carry your sounds and keys over. Soundpad and Resound organize sounds differently, so the import works a little differently from [the one from Soundux](soundux.md):

- **Soundpad** keeps lists of sound files from anywhere on your PC and groups them into categories however you like.
- **Resound** plays the folders you add: each folder and each of its subfolders becomes a tab. See [Sound folders and tabs](sound-library.md).

So your sounds come with the folders they are in.

## Import your sounds

1. On the setup's welcome page, click **Import from Soundpad…** in the **Coming from Soundpad?** card.
2. Under **Sound lists**, choose the lists to carry over. **Current list** is what Soundpad shows right now. Lists you saved in Soundpad's folder or opened recently are listed too; **Choose another list…** adds a `.spl` file saved anywhere else.
3. Under **Folders**, choose the folders to add. Each shows how many of its sounds are in your lists:
   - A folder that holds mostly your lists' sounds starts **on**.
   - A folder where most sounds aren't in your lists (say, your Downloads folder with one sound you used) starts **off**, so it doesn't fill your phone with sounds you never picked. Turn it on if you want it anyway.
   - Folders already in your library say so and need nothing.
4. Choose what else to carry over, then click **Import and continue**. The setup goes on with its usual steps, so you can check everything.

:::tip Quit Soundpad first
If Soundpad is still running, both apps press your push-to-talk key and react to your keys. The import page warns you while it runs.
:::

## What carries over

| In Soundpad | In Resound |
|---|---|
| Sounds in your chosen lists | The folders they are in, as tabs |
| Hotbar | Favorites, also in the [in-game overlay](overlay.md) |
| Sound colors | The nearest of the phone's card colors |
| Auto keys that hold a key while a sound plays (push-to-talk) | Push-to-talk keys |
| Stop | A [stop key](push-to-talk.md#stop-keys) |
| Pause/resume | A [pause key](push-to-talk.md#pause-keys). In Soundpad it switches between pause and play; in Resound, sounds pause while you hold it, so you can talk on another channel |

Soundpad only presses its auto keys for sounds you start with a hotkey, unless you changed that. Resound holds push-to-talk for every sound played for others.

## What doesn't carry over

- **Categories.** Resound's tabs are your folders. To group sounds differently, arrange them in folders and subfolders.
- **Keys with Ctrl, Alt, Shift or Win.** Soundpad doesn't document how it stores these, so Resound leaves them out rather than guess. The import page names them; set them in the setup's push-to-talk step or in Settings → Keybinds.
- **Hotkeys of single sounds.** Resound plays sounds by key from the [in-game overlay](overlay.md) of your favorites.
- **Formats Resound doesn't play.** Resound plays MP3, WAV, FLAC and OGG; Soundpad also plays M4A, WMA, AAC and more. Convert those, or cut them into a supported format with the [audio editor](audio-editor.md).
- **Sounds whose files are gone**, and streams from the internet.
- **Audio devices and volume.** The setup's audio step sets them up with VB-Cable. Resound [evens out the loudness](volume.md) of your sounds, as Soundpad's "normalize" option does.

## Your settings are safe

- The import only reads Soundpad's sound lists and its settings, which Windows keeps for your user account; it needs no administrator rights. Soundpad keeps working as before.
- Settings you have in Resound already are kept: favorites are added, a color set in Resound stays, keys are added next to yours. A key that already does something else in Resound is left out, and the first step says so.
- If Resound was set up before, it [backs up](backup.md) your current settings first (`Resound-backup-…-before-soundpad.zip`).

To import later: Settings → **Run the guided setup again**, then **Import from Soundpad…** on its welcome page.
