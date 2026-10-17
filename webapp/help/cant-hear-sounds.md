# I don't hear the sounds myself

Your sounds play to your own headphones or speakers through **You hear**, separately from what others hear.

## Check the device

In the audio flow (home page, or Settings → Audio & voice chat), look at **You hear**:

- Is the right device chosen? **System default** is Windows' default playback device.
- Is its volume up?
- Click **Test sound**: the bar under You hear should move.

## After plugging in headphones or changing devices

Resound keeps playing to the device it opened at start. After plugging in a headset, switching Windows' default device, or when a line shows **Output failed**, click **Restart outputs** under Settings → Audio & voice chat.

## Another app is using the device

If hovering **Output failed** says *another app is using this device exclusively*, a program has taken the device for itself, often an audio mixer such as Voicemeeter or SteelSeries Sonar. Either choose one of that program's own devices under **You hear** (such as *Voicemeeter Input* or *Sonar - Gaming*), or stop it from taking the device: press <kbd>Win</kbd>+<kbd>R</kbd>, run `mmsys.cpl`, open the device's **Properties** → **Advanced**, and turn off **Allow applications to take exclusive control of this device**. Then click **Restart outputs**.

## Muted in Windows

Resound appears in Windows' volume mixer like any app. Settings → System → Sound → **Volume mixer**: check that Resound isn't muted or turned down.

## Sounds get quieter during calls

- **Windows ducking:** Settings → Audio & voice chat → **Windows sound** → **Communications** tab → **Do nothing**.
- **Discord's attenuation:** User Settings → Voice & Video → **Attenuation**: set it to 0%. Otherwise Discord turns down other apps, Resound included, while someone talks.

## Others hear it, you don't (or the other way round)

- **Preview** in a sound's settings plays to you only, on purpose.
- A Bluetooth headset can sound off or go silent while its microphone is in use. See [Sound quality and volume](sound-quality.md#bluetooth-headsets).
