# The Resound panel

Resound's controls on the PC live in a panel that opens from its tray icon, next to the clock.

## Open and close it

- **Open:** click the tray icon, or start Resound again from the Start Menu. From the phone app, a long press on the logo in the bottom bar opens or closes it.
- **Close:** **✕ CLOSE** on the tab above the panel, <kbd>Esc</kbd>, or the tray icon. <kbd>Esc</kbd> first goes back one page, as does your mouse's back button (which never closes the panel).
- **Quick menu:** right-click the tray icon for **Show panel**, **Quit** and **Reset panel position**, whatever page the panel is on.

## Move it

The panel opens next to the tray icon. If it covers something you need, drag it by **DRAG TO MOVE** on its tab to anywhere on your screens. It opens there from then on, also after a restart, and always stays fully on screen.

To put it back next to the tray icon, double-click **DRAG TO MOVE**, or right-click the tray icon and choose **Reset panel position**. If the screen you moved it to is unplugged, the panel opens next to the tray icon until that screen is back.

The panel stays open when you click into another window, so you can drag folders from Explorer or sounds from your browser onto it. It reopens on the page you left.

## The home page

From top to bottom:

- **Status:** “Ready”, the number of things that need attention, or what's playing, with a short bar above it that shows how far the sound has played. **Update** appears here when a new version is out.
- **Problems**, if there are any, such as a failed output or voice chat that isn't set up. Click one to get to its fix.
- **Sound library** and **Remote**, side by side. **Folders** leads to your folders and to [importing sounds](import-sounds.md); its top right button refreshes the library. **Web app** leads to pairing; its top right button opens the app in a window on this PC.
- **The audio flow**, below.
- **Help**, **Settings** and **Quit**. Quit stops Resound: sounds stop and the phone can't connect until you start it again.

## The audio flow

The audio flow draws where your audio goes: from your microphone and the soundboard, through VB-Cable, to what others hear and what you hear. Lines light up with the signal and the bars show live levels.

- **Your microphone:** choose it here. **Automatic** uses Windows' default recording device.
- **Soundboard:** **Test sound** plays a short chime to both outputs.
- **Others hear:** always CABLE Output, the virtual microphone you choose in your apps; hover over its name to see where. The slider with the speaker icon sets how loud your sounds are for others.
- **You hear:** your headphones or speakers, with its own volume. **System default** uses Windows' default playback device.

Markers on the lines show what's going on. Hover over one to see why, click it to get to the fix:

| Marker | What it means |
|---|---|
| **Not routed** | Your voice doesn't reach the cable. Click it to turn on Listen to this device |
| **VB-Cable missing** | Voice chat needs VB-Cable. Click it to install it |
| **Restart needed** | VB-Cable is installed; restart your PC to finish |
| **Muted for sounds** | On purpose: Resound mutes your mic while a long sound plays, see [mic mute](push-to-talk.md#mute-your-microphone-while-sounds-play) |
| **Muted** | Your microphone is muted in Windows or on your headset |
| **Holding …** | Resound holds your push-to-talk key while sounds play |
| **Paused**, **Talking**, **Pause key held** | Playback is paused from the phone, by talk-through or by a pause key |
| **Elsewhere** | Sounds for others go to another device than VB-Cable (Advanced) |
| **Output failed** | A device stopped working, was unplugged, or another app uses it exclusively (hover to see which). Click it to restart the outputs |
| **Not connected** | The microphone you chose is unplugged |

:::note The microphone-in-use icon
While the audio flow is on screen, Resound listens to your microphone and to CABLE Output to draw the bars, so Windows shows its microphone icon. Nothing is recorded or kept, and it stops when you close the panel.
:::

## Settings

| Page | What's there |
|---|---|
| Audio & voice chat | The audio flow, VB-Cable, mic mute, **Restart outputs**, **Windows sound**, Advanced output choice |
| Keybinds | Push-to-talk, pause and stop keys and mouse buttons, everywhere or for one game |
| Sound library | Folders, importing, loudness |
| In-game overlay | The overlay's key or mouse button and screen edge |
| Remote App | Pairing, the firewall, signing out devices |
| App & startup | Version, start with Windows, updates, uninstall |
| Backup & restore | Backups of your settings, and a zip of your sound files |
| Diagnostics | Devices, latency test, log and data folders |

At the bottom: **Run the guided setup again**.
