# Push-to-talk, pause and stop keys

Many games only send your voice while you hold a push-to-talk key, so a sound played without it isn't heard. Resound can hold that key for you while sounds play, mute your real microphone meanwhile, and react to keys of its own from inside your game. All of this is under Settings → **Push-to-talk**.

:::viz ptt-timeline
While a sound plays, Resound holds your push-to-talk key and, if you turn it on, mutes your microphone. Holding a **pause key** pauses the sound and lets go of both, so you can talk. Letting go resumes the sound; when it ends, everything is let go.
:::

## Set your game's push-to-talk key

1. Pick a key from **Pick a push to talk key**, or click **Record** and press the key your game uses. Mouse buttons work too: press them over the panel.
2. Bind the same key in your game. To make that easy, click **Send it to the game in 5 s**, switch to the game and click its keybind field: Resound presses the key for you.
3. Keep **Hold it while sounds play** on.

:::tip Use a spare key
Pick a key you never press yourself, such as **Pause**, **Scroll Lock** or a numpad key, and bind it in the game as a second push-to-talk key. Then Resound's key and yours never get in each other's way. F13–F24 don't work in every game.
:::

No push-to-talk in your game (voice activity)? Leave the key empty. Make sure the game's voice activation picks up sounds, see [Sounds cut out](discord.md#input-sensitivity).

## Mute your microphone while sounds play

Off by default. Turn it on under Settings → Audio & voice chat → **Mute my microphone while sounds play**. Others then hear the sound clearly, without you talking or typing in the background. The audio flow shows **Muted for sounds** meanwhile.

**Minimum sound length** (right below the switch, 5 seconds at first) decides which sounds mute it:

- A sound **longer** than it mutes your microphone while it plays: music, long clips.
- **Shorter** sounds leave it open, so you can keep talking over quick effects.
- A sound whose length Resound doesn't know yet mutes it, to be safe.

Each sound counts on its own: short sounds played one after another never add up. While a long sound plays, your microphone stays muted, also when short ones play alongside; it's back as soon as no long sound plays anymore.

Use **−** and **+** to change the length, or click the value to type one, such as `2.5`. Below 1 second it reads **Always**: then every sound mutes your microphone.

## Talk over a sound

- **Talk-through** in the phone app: hold the microphone button next to STOP. The sounds pause and your microphone is live; push-to-talk stays held.
- **Pause keys** on the PC (below): the sounds pause and push-to-talk is let go, for example to talk on another channel.

## Pause keys

Under **Hold to pause**, click **Add key** and press a key or a combination. While you hold it, sounds pause and Resound lets go of push-to-talk and your mic. Letting go resumes the sounds and presses push-to-talk again.

## Stop keys

Under **Stop keys**, add a key or combination that stops all sounds at once and lets go of push-to-talk. Useful when a sound has to end now.

You can add up to 8 pause keys and 8 stop keys, keyboard keys only. A key can't be both, and it can't be your push-to-talk key. Pause and stop keys work in any game; the game still gets the key as well.

Keys not working in a game? See [Keys don't work in a game](keys-in-games.md).
