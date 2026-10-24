# The audio editor

Resound can cut sounds out of videos and other files, and trim the sounds already in your library. It does this with [audiocutter.online](https://audiocutter.online), a free audio editor that runs in your browser. Resound opens it in a window of its own, with your file already loaded, and saves what you cut straight into your library.

## What you can do

- **Cut sounds from a video or another audio file.** Drop a video, or audio in a format Resound doesn't play directly, onto a folder on the Sound library page. Cut as many clips from it as you like: each one becomes a sound in that folder.
- **Trim or edit a sound in your library.** Open a folder's sound list and click a sound's **scissors**. Save, and the edited sound replaces the original; the editor window closes a moment later (press **Keep editing** to stay).

The editor itself can do more than trimming: you can select and cut parts, add fades and change the volume. Whatever it plays back is what Resound saves.

## The first time: what you agree to

The first time the editor opens in a browser, the window explains what the editor is before anything happens:

- The editor is **audiocutter.online**, a third-party editor, not made by Resound. It loads from its own website into the window.
- Your file is processed **locally, in your browser**. It isn't uploaded: Resound hands it to the editor inside your PC, and the editor works on it there. audiocutter.online says the same of itself: audio never reaches its servers.
- `getresound.app`, the site the window comes from, never receives your file, its name or your clips.

Click **Accept and open the editor** to continue, or **More info** to read this page. You're asked once per browser.

:::note The browser's question
Right after you accept, your browser may ask whether `getresound.app` may **access apps on this device**. Allow it: that's how the file gets from Resound into the window, over a connection inside your PC. Browsers ask this once per site.
:::

## Cut sounds from a video

1. On the PC, open Settings → **Sound library** (or click **Folders** on the home page).
2. Drag a video or audio file from Explorer onto a folder. While it's over the folder, the folder reads **Drop to cut in the editor**.
3. A browser window opens with the file loaded. In the panel, the folder reads **Editing in your browser…**.
4. Select the part you want (drag across the waveform) and press **Add to Resound**. The clip is saved into the folder, and the usual card opens in the panel to name it, preview it or discard it.
5. Select the next part and press **Add to Resound** again for another clip. Each clip you add shows as a chip in the window's top bar; the newest is highlighted.
6. When you're done, press **Done** at the top of the window. The window closes, and the clips you added stay in the folder. **Stop editing** under the folder in the panel does the same.

Up to three files open at once from one drop, each in its own window.

## Trim a sound in your library

1. On the Sound library page, click a folder to list its sounds.
2. Click the **scissors** next to a sound. The editor opens with it; the sound's line reads **Editing…**.
3. Select the part to keep and press **Save to Resound**. The trimmed sound replaces the file.

The sound keeps its name, its format (an MP3 stays an MP3) and its settings: favorite, volume, color and emoji. Resound measures its length and loudness again. You can keep editing and save again; each save replaces the sound once more.

:::warning Replacing is final
The edited sound overwrites the file. To keep the original, copy it in Explorer first.
:::

## What gets saved

**Add to Resound** and **Save to Resound** save what the editor would download:

- the **selection**, if you selected a part;
- otherwise the selected fragment, or the **whole file**.

New clips are saved as **FLAC**: lossless, so cutting doesn't cost quality, and playable at any sample rate. A sound you trim keeps its own format.

The editor's own **Download file** button works too. Drag the downloaded file from your browser's download list onto a folder to add it.

## Supported files

Resound opens these in the editor:

| Kind | Formats |
|---|---|
| Video | MP4, M4V, MOV, MKV, WEBM, AVI, WMV, FLV, 3GP, MPG, MPEG, TS, MTS |
| Audio | M4A, AAC, OPUS, WMA, AIFF, AIF, AMR, MKA, AC3, CAF, WEBA, OGA |
| Sounds Resound plays directly | MP3, WAV, FLAC, OGG (dropped onto a folder, they're copied in; their scissors open them in the editor) |

The editor reads files with FFmpeg, in your browser. A file without an audio track, or one it can't read, gets a message in the editor. Files can be up to 1 GB; the editor reads the whole file into memory.

## Good to know

- **One audio track.** The editor uses a video's first audio track. To cut from another one (a second language, a commentary), save that track as a file of its own first, for example with a video converter.
- **Esc stays in the editor.** While a file is open in the editor, <kbd>Esc</kbd> leaves the panel on the Sound library page, so you can use it freely in the editor. Close the panel with its close button; it opens on the same page again.
- **Show editor** under the folder brings the editor window back if you closed or lost it.
- **Colors and theme.** The window tints the editor in Resound's colors. The editor follows your Windows light or dark setting; its sun and moon button switches it, and it remembers your choice.
- **A small window** scrolls with the scrollbar on the right. The editor needs about 500 pixels of height.
- **It needs the internet.** The editor loads from audiocutter.online each time.

## When something goes wrong

| What you see | Why | What to do |
|---|---|---|
| Can't reach Resound | Resound was closed, editing was stopped in the panel, or the browser blocks the connection | Check that Resound is running, then drop the file again or click the scissors again |
| Your browser blocks this page from reaching Resound | You answered **Block** to the browser's question | Open the site settings of `getresound.app` (the icon at the left of the address bar, or in the window's title bar), allow access to apps on this device, and reload the window |
| The editor stays empty | No internet, or audiocutter.online is down | Reload the window later |
| The clip is larger than 100 MB | A very long clip | Select a shorter part |
| Resound can't play this clip | The editor sent something Resound couldn't read | Try again; if it keeps happening, use **Download file** and add the file yourself |
| has no audio track | The video has no sound | Use another file |

## Privacy

| Who | What they get |
|---|---|
| Resound | Your file stays where it is. Resound serves it to the editor window over a connection inside your PC (`127.0.0.1`), open only while you edit, and saves what you add. |
| `getresound.app` | The editor window's page itself, nothing about your file. The page is set up so it can't contact `getresound.app` at all. |
| audiocutter.online | The editor's own website: it sees your IP address and browser, like any site. Your file is processed in your browser, not uploaded. |
| `unpkg.com` | The editor loads its audio converter (FFmpeg) from there. |

See the [privacy policy](https://github.com/motivated2die/resound/blob/main/PRIVACY.md) for everything Resound connects to.

## Credits

The editor is [audiocutter.online](https://audiocutter.online), made by its own team, independent of Resound. It's free to use, runs entirely in the browser, and offers the postMessage interface Resound uses to hand over files and take the edits back. Thanks to its makers for building a fast, private editor and opening it to other apps. Their [guides](https://audiocutter.online/guides/) explain the editor's features in depth; for questions about the editor itself, use the feedback form on audiocutter.online.

Resound uses audiocutter.online's free, hosted editor as it is offered. If audiocutter.online changes or goes offline, cutting sounds stops working until Resound is updated; your library isn't affected.
