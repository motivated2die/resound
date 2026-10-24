# Keybinds: push-to-talk, pause and stop

Many games only send your voice while you hold a push-to-talk key, so a sound played without it isn't heard. Resound can hold that key for you while sounds play, mute your real microphone meanwhile, and react to keys of its own from inside your game: **pause keys** and **stop keys**. Keyboard keys and mouse buttons both work. All of this is under Settings → **Keybinds**.

:::viz ptt-timeline
While a sound plays, Resound holds your push-to-talk key and, if you turn it on, mutes your microphone. Holding a **pause key** pauses the sound and lets go of both, so you can talk. Letting go resumes the sound; when it ends, everything is let go.
:::

## The Keybinds page

Each function has a card: **Push-to-talk**, **Pause** and **Stop**. The **Global** cards apply in every app; below them, **App keybinds** lists the games that have keys of their own.

| On a card | Does |
|---|---|
| **+** | Records a key: press a key, a combination (such as <kbd>Ctrl</kbd> + <kbd>F</kbd>) or a mouse button. Or choose one from the list that opens. <kbd>Esc</kbd> or **✕** cancels |
| **✕** on a key | Removes that key |
| ⓘ | Says what the function does |
| A greyed-out, flat card | No key set: the function is off |

The ⓘ next to each function and option explains it in a sentence; **Help** at the top opens this article.

## Set your game's push-to-talk key

1. Settings → **Keybinds** → **+** on **Push-to-talk**. Press the key your game uses, or pick one from the list.
2. Bind the same key in your game. To make that easy, click the **keyboard button** on the card, switch to the game and click its keybind field: Resound presses the key for you 5 seconds later.
3. Keep **Hold push-to-talk while sounds play** on (under **Options**).

:::tip Use a spare key
Pick a key you never press yourself, such as **Pause**, **Scroll Lock** or a numpad key, and bind it in the game as a second push-to-talk key. Then Resound's key and yours never get in each other's way. F13–F24 don't work in every game.
:::

You can add up to 4 push-to-talk keys. Resound holds them all together, for example your game's key and Discord's.

No push-to-talk in your game (voice activity)? Leave it empty. Make sure the game's voice activation picks up sounds, see [Sounds cut out](discord.md#input-sensitivity).

## Mouse buttons

Every keybind takes mouse buttons too: the middle button (**Mouse 3**) and the side buttons (**Mouse 4** and **Mouse 5**), alone or with keys, such as <kbd>Ctrl</kbd> + Mouse 4. Press them with the pointer over the panel while it records. The left and right buttons can't be bound: you need them to aim and to click.

Pause and stop buttons work like keys: the game still gets the click.

## Pause keys

On the **Pause** card, click **+** and press a key, a mouse button or a combination. While you hold it, sounds pause and Resound lets go of push-to-talk and your mic, for example so you can talk on another channel with that key. Letting go resumes the sounds and presses push-to-talk again.

## Stop keys

On the **Stop** card, add a key or button that stops all sounds at once and lets go of push-to-talk. Useful when a sound has to end now.

You can add up to 8 pause keys and 8 stop keys. Pause and stop keys work in any game; the game still gets the key as well.

## Keybinds for one app

Games bind different keys: one game's push-to-talk may be <kbd>V</kbd>, another's Mouse 4. An app can have keys of its own, which work while it is in front.

1. On the Keybinds page, open **Add a running app** and choose the game. Start the game first, so it's in the list.
2. Its page shows the same three cards. Each one shows the **global** keys, greyed, until you give it keys of its own with **+**.

For each function, the app either:

- **keeps the global keys** (nothing set: the card says *Global*),
- **has its own keys**, which replace the global ones in that app, or
- has **None**: click **⊘** on its card (or choose None at the top of the list while recording), and the function is off in that app.

The **↺** arrow on a card goes back to the global keys. Only the functions you set change; everything else keeps working as everywhere else.

:::viz keybind-scopes
Valorant has its own push-to-talk key and no pause key, and keeps the global stop key. In Discord (or any other app) the global keys work.
:::

Good to know:

- Resound follows the app in front. Clicking the Resound panel doesn't count: the keys of the app before it stay.
- If you switch apps while a sound plays, Resound lets go of the old app's push-to-talk key and holds the new one's at once. A key both apps use stays held.
- A pause key you hold while switching keeps the sounds paused until you let go of it.
- While a game with its own keys runs, the home page says so under the logo, for example *Ready · Valorant keybinds*.
- The trash button on an app's page removes its keys; the global ones apply there again.

## Options

- **Hold push-to-talk while sounds play**: off, Resound never presses push-to-talk for sounds. Talk-through on the phone always holds it.
- **My push-to-talk pauses sounds** (on by default, see [below](#press-your-push-to-talk-key-yourself)): holding your push-to-talk key yourself pauses the sounds so you can talk.
- **Let go of push-to-talk first** (shown while you have push-to-talk and a pause or stop key): while sounds hold push-to-talk, a pause or stop key waits a moment for Resound to let go of it, so the game doesn't end your own push-to-talk too. Turn it off if a game ignores your key while sounds play.

## One key, one job

Wherever a key works, it does one thing: it can't be both a pause and a stop key, and it can't be a push-to-talk key Resound presses. That counts for the global keys and for every app that keeps them; Resound says so under the card when you add one. Left and right <kbd>Ctrl</kbd>, <kbd>Shift</kbd> and <kbd>Alt</kbd> count as the same key.

## Mute your microphone while sounds play

Off by default. Turn it on under Settings → Audio & voice chat → **Mute my microphone while sounds play**. Others then hear the sound clearly, without you talking or typing in the background. The audio flow shows **Muted for sounds** meanwhile.

**Minimum sound length** (right below the switch, 5 seconds at first) decides which sounds mute it:

- A sound **longer** than it mutes your microphone while it plays: music, long clips.
- **Shorter** sounds leave it open, so you can keep talking over quick effects.
- A sound whose length Resound doesn't know yet mutes it, to be safe.

Each sound counts on its own: short sounds played one after another never add up. While a long sound plays, your microphone stays muted, also when short ones play alongside; it's back as soon as no long sound plays anymore.

Use **−** and **+** to change the length, or click the value to type one, such as `2.5`. Below 1 second it reads **Always**: then every sound mutes your microphone.

## Talk over a sound

- **Your own push-to-talk key**: just hold it and talk, see below.
- **Talk-through** in the phone app: hold the microphone button next to STOP. The sounds pause and your microphone is live; push-to-talk stays held.
- **Pause keys** on the PC (above): the sounds pause and push-to-talk is let go, for example to talk on another channel.

## Press your push-to-talk key yourself

While a sound plays, Resound holds your push-to-talk key. You can still press that key yourself to talk, as always: with **My push-to-talk pauses sounds** on (the default), it works like a pause key.

1. You press your push-to-talk key: the sound pauses and your microphone is live. Your game keeps sending, now your voice.
2. You let go: your game stops sending, as it always does when the key goes up.
3. A moment later (0.15 s), the sound continues and Resound presses push-to-talk again, so others hear the rest of it.

Press the key again within that moment and the sound stays paused. Holding a pause key at the same time keeps the sound paused until you let go of both.

With the switch off, your key is just a key: but when you let go of it while a sound plays, many games end push-to-talk, and the rest of the sound isn't heard until the next one starts. The switch shows under **Options** while Resound holds push-to-talk for sounds.

Keys not working in a game? See [Keys don't work in a game](keys-in-games.md).
