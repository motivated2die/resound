# Sounds cut out in Discord

Your friends hear sounds only in bits, choppy, muffled, very quiet, or music is cut off after a second. That's almost always Discord's voice processing: it is built to keep only a human voice, so it treats music and sound effects as background noise and removes them.

:::viz discord-processing
Noise suppression lets a voice through and filters out everything else, and your sounds count as everything else. Switch it off and they arrive as they play.
:::

## Turn off voice processing

In Discord: **User Settings** → **Voice & Video**.

1. **Input Device:** CABLE Output (VB-Audio Virtual Cable).
2. **Input Profile:** **Custom**. The Voice Isolation profile switches noise suppression on.
3. **Noise Suppression:** **None** (not Krisp or Standard).
4. **Echo Cancellation:** off.
5. **Automatic Gain Control:** off. It turns sounds up and down unevenly.

Discord renames and moves these settings now and then; look for the same words under Voice Processing or the advanced voice settings.

:::note What about background noise?
Without noise suppression Discord sends your microphone as it is. A headset microphone close to your mouth usually sounds fine. Resound can also mute your microphone while longer sounds play ([mic mute](push-to-talk.md#mute-your-microphone-while-sounds-play)), so your room doesn't mix into them.
:::

## Input sensitivity

With **Voice Activity**, Discord only sends sound louder than a threshold. Quiet parts of a sound, and the start of soft ones, fall below it and are cut.

- Turn off **Automatically determine input sensitivity**.
- Drag the slider far to the left, so that quiet sounds still light up the bar (for example around −60 dB).

Or use **Push to Talk** in Discord and let Resound hold the key while sounds play: [Keybinds](push-to-talk.md).

## Other apps

Games, Teams, Zoom, Google Meet and TeamSpeak have similar filters (noise suppression, “background noise”, voice activation). Turn them off or down the same way. Microphone software such as NVIDIA Broadcast only filters your microphone before it reaches the cable, not your sounds.
