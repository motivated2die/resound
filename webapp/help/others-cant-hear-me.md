# Others can't hear my voice

Your sounds come through, but your voice doesn't, it's muted, or friends hear you twice. Since apps listen to CABLE Output, your voice has to reach VB-Cable first.

## The audio flow says “Not routed”

Your microphone doesn't reach the cable yet. Click the **Not routed** marker, or **Connect to VB-Cable** under Settings → Audio & voice chat. Resound turns on Windows' **Listen to this device** for your microphone and plays it into the cable. Windows asks for administrator approval once.

If you plugged in another headset, choose it under **Your microphone** and connect again.

## The wrong microphone

**Automatic** uses Windows' default recording device. If that's your webcam instead of your headset, choose your microphone by name in the audio flow. Talk: its bar should move.

## Muted

- **Muted** on the line: the microphone is muted in Windows or with your headset's mute button. Unmute it there.
- **Muted for sounds**: that's on purpose while sounds play; your mic is back when they end. You can turn it off: [mic mute](push-to-talk.md#mute-your-microphone-while-sounds-play).

## The bar doesn't move: Windows' privacy settings

If the panel says your microphone's level can't be shown, Windows keeps desktop apps from the microphone. Click **Open microphone privacy settings** and turn on **Let desktop apps access your microphone**. Discord and games need this too.

## Still muted after Resound closed unexpectedly

If Resound was ended in Task Manager or crashed while a sound played, your microphone can stay muted and your push-to-talk key held:

- unmute the microphone in Windows (Settings → System → Sound → your microphone, or your headset's button);
- press your push-to-talk key once to release it.

## Friends hear me twice or with an echo

- The app records both your microphone and CABLE Output, which already contains your voice. Use only CABLE Output, for example in OBS.
- Don't turn on **Listen to this device** for the microphone yourself towards your headphones: then you hear yourself. Resound sets it to the cable; to undo it, see [Remove VB-Cable and Windows changes](remove-vb-cable.md#turn-off-listen-to-this-device).
