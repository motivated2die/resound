# Keys don't work in a game

Resound presses your push-to-talk key and listens for its pause, stop and overlay keys the same way any keyboard tool does. When that doesn't reach a game, it is usually one of these.

## The game runs as administrator

Windows doesn't let programs exchange keys with programs running as administrator. If the game, its launcher or its anti-cheat runs elevated, Resound's push-to-talk doesn't reach it and Resound doesn't see your hotkeys while it has the focus.

- Start the game normally, not with **Run as administrator**, and check its shortcut's **Compatibility** tab: **Run this program as an administrator** must be off.
- To check: Task Manager → **Details** → right-click a column → **Select columns** → **Elevated**.

## Push-to-talk isn't bound in the game

- The key in Resound must be the game's push-to-talk key (or a second one). Use **Send it to the game in 5 s** and click the game's keybind field.
- F13–F24 aren't accepted by every game; choose Pause, Scroll Lock or a numpad key instead.
- A few games ignore keys pressed by other programs. Use voice activation in that game, or play your sounds in Discord instead.

## Pause or stop keys don't react

- A combination counts only while all its keys are held. Left and right <kbd>Ctrl</kbd>, <kbd>Shift</kbd> and <kbd>Alt</kbd> count as the same key.
- Keys can't be shared: a pause key can't also be a stop key or your push-to-talk key. Resound says so when you record one.
- Mouse buttons: only the middle and side buttons (Mouse 3–5) can be keybinds. Mouse software that turns a side button into a key or a macro sends that instead; bind what it sends.

## Keys for one game don't apply

[Keys of a game's own](push-to-talk.md#keybinds-for-one-app) only work while that game is in front.

- Check the game is listed under **App keybinds** with the functions you set (its line says which, such as *Push-to-talk, pause*). A function that says *Global* on its card uses the global keys.
- Resound knows a game by its program file. If the game starts through a launcher, add the game itself while it runs, not the launcher.
- A function set to **None** is off in that game: its global key does nothing there. The **↺** arrow brings the global keys back.

## The overlay doesn't show

- Check that it has a key: Settings → **In-game overlay**. Try **Show the overlay** there.
- It shows on the main screen. Move the game there, or make that screen the main display in Windows.
- A game in true exclusive fullscreen covers it: switch the game to **borderless** or **windowed** fullscreen.
