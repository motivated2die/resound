# Use Resound in Discord and games

Every app chooses its own microphone. In each app where others should hear your sounds, pick **CABLE Output (VB-Audio Virtual Cable)** instead of your headset or webcam microphone. Your voice still comes through: Resound routes it into the cable (see [Set up voice chat](voice-chat.md)).

## Discord

1. Open **User Settings** (the gear next to your name) → **Voice & Video**.
2. Set **Input Device** to **CABLE Output (VB-Audio Virtual Cable)**. Leave **Output Device** on your headphones.
3. Turn off Discord's noise suppression, echo cancellation and automatic gain control, and set the input sensitivity by hand. Discord's voice processing is the most common reason sounds cut out: see [Sounds cut out in Discord](discord.md).
4. If you use **Push to Talk** in Discord, set the same key in Resound, so it holds the key while sounds play: [Push-to-talk](push-to-talk.md).

## Games

- In the game's audio or voice chat settings, choose **CABLE Output** as the microphone or input device.
- If the game has push-to-talk, set its key in Resound too.
- Steam's voice chat: Steam → **Settings** → **Voice** → **Voice Input Device**: CABLE Output.

### Games without a microphone choice

Some games always use Windows' default recording device. Make CABLE Output the default:

1. First choose your microphone by name in Resound's audio flow, not **Automatic**. (Automatic follows Windows' default recording device.)
2. Settings → Audio & voice chat → **Windows sound** opens Windows' Sound dialog on the Recording tab.
3. Right-click **CABLE Output** → **Set as Default Device**, and again → **Set as Default Communication Device**.

## Streaming with OBS

- To record your voice and sounds together, add an **Audio Input Capture** source with the device **CABLE Output**. Don't capture your microphone separately as well, or your voice is in the stream twice.
- Your own sounds also play on **You hear** (your headphones), which OBS's Desktop Audio picks up if it is the same device. Mute one of the two in OBS, or you'll hear the sounds twice on stream.

## Other voice apps

TeamSpeak, Mumble, Teams, Zoom, Google Meet and others work the same way: choose **CABLE Output** as the microphone and turn off their noise suppression or “background noise” filter where they have one.

:::tip Test in the app itself
Most apps have a microphone test. Start it, then play a sound from your phone: if the app's meter moves and you hear the sound back, others will hear it too.
:::
