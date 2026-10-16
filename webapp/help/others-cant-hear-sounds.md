# Others can't hear my sounds

You hear your sounds, but your friends in Discord or the game don't. Go through these checks in order; most problems are solved by the first two. The panel's audio flow (Settings → Audio & voice chat) helps: play **Test sound** and watch where the signal stops.

- [ ] **VB-Cable is installed and your PC was restarted.** The audio flow shows no **VB-Cable missing** or **Restart needed** marker. See [Set up voice chat](voice-chat.md).
- [ ] **Your app's microphone is CABLE Output.** In Discord: User Settings → Voice & Video → Input Device → **CABLE Output (VB-Audio Virtual Cable)**. The same in every game. This is the most common cause. See [Use Resound in Discord and games](apps.md).
- [ ] **The bar under “Others hear” moves** when you play Test sound. If it doesn't: a marker on the line says why. **Elsewhere** means sounds for others go to another device (Settings → Audio & voice chat → Advanced → **Automatic: VB-Cable**); **Output failed** means click **Restart outputs**.
- [ ] **The Others hear volume** isn't turned down to nothing.
- [ ] **Push-to-talk:** if the game or Discord uses push-to-talk, set the same key in Resound and keep **Hold it while sounds play** on. See [Push-to-talk](push-to-talk.md).
- [ ] **Voice processing and input sensitivity** let your sounds through. Discord's noise suppression and automatic sensitivity often cut sounds off. See [Sounds cut out in Discord](discord.md).
- [ ] **The app was restarted after installing VB-Cable.** Some apps only see new devices after a restart.
- [ ] **CABLE Output isn't muted in Windows.** Settings → Audio & voice chat → **Windows sound** → Recording tab → **CABLE Output** → Properties → Levels: not muted, at 100. Check **CABLE Input** on the Playback tab the same way.

If they don't hear your voice either, also see [Others can't hear my voice](others-cant-hear-me.md). Still stuck? [Diagnostics and reporting a problem](diagnostics.md).
