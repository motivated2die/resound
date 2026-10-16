# Sound quality and volume

## Bluetooth headsets

As soon as an app uses a Bluetooth headset's microphone, Windows switches the headset to its hands-free mode: everything you hear sounds like a phone call, and the device may appear twice in Windows (“Headphones” and “Headset”). Resound uses your microphone too, for voice chat and to draw the audio flow's meters.

- Best: use a separate microphone (USB, webcam or your headset's USB dongle) and keep the Bluetooth headset for listening only.
- In Resound choose that microphone under **Your microphone**, and the headset's **Headphones** (stereo) device under **You hear**.

## Sounds are distorted for others

- Lower **Others hear** in the audio flow. Sounds far louder than your voice clip in voice chat.
- Keep **Even out the loudness of sounds** on (Settings → Sound library).
- Turn off automatic gain control in Discord or the game: see [Sounds cut out in Discord](discord.md).
- In Windows' Sound dialog (Settings → Audio & voice chat → **Windows sound**), set the level of **CABLE Output** (Recording) and **CABLE Input** (Playback) to 100, without enhancements.

## Some sounds are much louder than others

Turn on **Even out the loudness of sounds**. For single sounds, change their own volume on the phone (long-press the button → Volume Adjustment). See [Volume and loudness](volume.md).

## Sounds play late

Resound starts a sound within a few milliseconds of your tap; the Wi-Fi only carries the tap. Delays usually come from the voice chat itself. To measure Resound's part, use the latency test under Settings → **Diagnostics**.

## My voice sounds different

Your voice passes Windows' **Listen to this device**, which adds a small delay but doesn't change it. If it sounds processed, look at your microphone's own software, Windows' audio enhancements for the microphone, or the voice app's filters.
