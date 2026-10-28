# Switch from Soundpad

If Soundpad is set up on your PC, the [guided setup](guided-setup.md) offers to carry your sounds and settings over. Soundpad and Resound organize sounds differently:

- **Soundpad** keeps lists of sound files from anywhere on your PC and groups them into categories however you like.
- **Resound** plays the folders you add: each folder and each of its subfolders becomes a tab. See [Sound folders and tabs](sound-library.md).

So you choose how your sounds come over: **by category** or **by folder**.

## Import your sounds

1. On the setup's welcome page, click **Import from Soundpad…** in the **Coming from Soundpad?** card.
2. Under **Sound list**, choose the list to carry over. The list Soundpad shows right now is chosen at first and marked **current**; lists you saved in Soundpad's folder or opened recently are in the list too. The folder button to its left adds a `.spl` file saved anywhere else. Below, you see what the chosen list holds. One list at a time, so two lists can't disagree about your sounds.
3. Under **Your sounds**, choose **By category** (chosen at first) or **By folder** (see below).
4. Choose what else to carry over, then click **Import and continue** (by category: **Copy, import and continue**). The setup goes on with its usual steps, so you can check everything.

:::tip Quit Soundpad first
If Soundpad is still running, both apps press your push-to-talk key and react to your keys. The import page warns you while it runs.
:::

### By category

Resound copies your sounds into a folder per category, so your tabs are your Soundpad categories, with the sounds in Soundpad's order.

1. Under **Copy to**, click **Choose a folder…** and choose a new or empty folder, for example `Music\Soundpad sounds`. Resound adds it to your library.
2. The page says how many files the copy makes, how much room they take and how much is free on that drive. A sound in several categories is copied into each of them.
3. When you continue, Resound copies the sounds and shows how far it got. **Stop copying** ends it; nothing is carried over then, and the files copied so far stay in the folder (copying there again skips them).

:::warning The copies take room
Your sounds stay where they are, so Soundpad keeps working, and the copies take as much room again. If the drive doesn't have enough, the page says so and you can choose a folder on another drive.
:::

How the folders are named:

- A category's folder has its name, without characters Windows doesn't allow in folder names (`\ / : * ? " < > |` become `_`).
- Two categories whose names differ only in capitals get "Fun" and "Fun (2)".
- Sounds in no category go into **Other sounds**.
- Tabs are sorted by name, so their order may differ from Soundpad's categories. Sounds in a tab keep Soundpad's order, until you sort or arrange them on your phone.

### By folder

Resound adds the folders your sounds are in, as they are. Nothing is copied or moved.

- All the folders are on at first. Next to each name, you see how many of its sounds are in your list, such as "4 of 50 sounds": Resound plays every sound in a folder you add, so a folder with other sounds brings those too. Point at a folder to see where it is. Switch off the folders you don't want.
- Folders already in your library say so and need nothing.
- Your categories and the order of sounds in them don't come over: your tabs are your folders.

## What carries over

| In Soundpad | In Resound |
|---|---|
| Sounds in your chosen list | By folder: the folders they are in. By category: copies in a folder per category |
| Hotbar | Favorites, also in the [in-game overlay](overlay.md). By category, a sound in several categories is a favorite once |
| Sound colors | The nearest of the phone's card colors |
| Auto keys that hold a key while a sound plays (push-to-talk) | Push-to-talk keys |
| Mute microphone while playing | [Mute your microphone while sounds play](push-to-talk.md#mute-your-microphone-while-sounds-play), with **Minimum sound length** set to **Always**, as Soundpad muted it for every sound |
| Stop | A [stop key](push-to-talk.md#stop-keys), with Ctrl, Alt, Shift or Win if you use them |
| Pause/resume | A [toggle pause key](push-to-talk.md#pause-keys): it switches between pause and play, as in Soundpad |

Soundpad only presses its auto keys for sounds you start with a hotkey, unless you changed that. Resound holds push-to-talk for every sound played for others.

## What doesn't carry over

- **Categories, by folder.** To keep them, import by category.
- **Keys of single sounds.** Resound plays sounds by key from the [in-game overlay](overlay.md) of your favorites.
- **Keys with Ctrl, Alt, Shift or Win, if Soundpad tells left and right apart** (its "distinguish left and right modifiers" option). The import page names such keys; set them in the setup's push-to-talk step or in Settings → Keybinds.
- **Formats Resound doesn't play.** Resound plays MP3, WAV, FLAC and OGG; Soundpad also plays M4A, WMA, AAC and more. Convert those, or cut them into a supported format with the [audio editor](audio-editor.md).
- **Sounds whose files are gone**, and streams from the internet.
- **Audio devices and volume.** The setup's audio step sets them up with VB-Cable. Resound [evens out the loudness](volume.md) of your sounds, as Soundpad's "normalize" option does.

## Your settings are safe

- The import only reads Soundpad's sound lists and its settings, which Windows keeps for your user account; it needs no administrator rights. Soundpad keeps working as before. By category, Resound writes only into the folder you choose.
- Settings you have in Resound already are kept: favorites are added, a color set in Resound stays, keys are added next to yours. A key that already does something else in Resound is left out, and the first step says so.
- If Resound was set up before, it [backs up](backup.md) your current settings first (`Resound-backup-…-before-soundpad.zip`).

To import later: Settings → **Run the guided setup again**, then **Import from Soundpad…** on its welcome page.
