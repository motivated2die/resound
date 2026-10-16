# Frequently asked questions

## Is Resound free?

Yes. Resound is free and open source software (GPL-3.0): no account, no subscription, no ads, nothing to unlock. VB-Cable, which it uses for voice chat, is donationware by VB-Audio.

## Does it work on Mac or Linux?

The soundboard itself needs Windows 10 or 11 (64-bit). The remote runs on any phone, tablet or computer with a current browser: Android, iPhone, iPad, Mac, Windows.

## Do I need a phone?

No. The app also runs on the PC itself, in a window of its own: the button next to **Phone remote** on the home page. In games, the [in-game overlay](overlay.md) plays your favorites with one key.

## Does the phone need internet?

Only to open the app the first time, to install it, and for updates. After that it needs only your PC, on the same network. It can't reach the PC over mobile data or from another network.

## Can several sounds play at once?

Yes, sounds overlap. **STOP** ends all of them, and a [stop key](push-to-talk.md#stop-keys) does the same from inside a game.

## Can I use Resound without VB-Cable?

Yes, to play sounds to your own speakers only, for example at a party or on stream. To be heard in voice chat or games, you need VB-Cable: [Set up voice chat](voice-chat.md).

## Which files can I play?

MP3, WAV, FLAC and OGG, of any length, songs included. Convert other formats (M4A, AAC, WMA) first. Long music may be cut by Discord's filters: [Sounds cut out in Discord](discord.md).

## Does Resound record me or send anything?

No. Your sounds, settings and library stay on your PC. The phone talks to the PC directly over your network, encrypted. Resound only goes online to check for updates (on GitHub), to download VB-Cable when you ask for it, and to download sounds you drag in. Details: [privacy policy](https://github.com/motivated2die/resound/blob/main/PRIVACY.md).

## Which Windows settings does Resound change?

Only what you ask for, each time with a prompt: it installs VB-Cable, turns on **Listen to this device** for your microphone, adds a firewall rule for your phone, starts with Windows if you turn that on, and mutes your microphone and holds your push-to-talk key while sounds play. See [Windows prompts and warnings](windows-prompts.md).

## Can I use Voicemeeter or another mixer?

Yes. Settings → Audio & voice chat → Advanced chooses where sounds for others play, for example a Voicemeeter input. Your voice then has to be mixed in by that mixer.

## How do I use Resound with OBS?

See [Streaming with OBS](apps.md#streaming-with-obs).

## Is it safe to download?

Every release is built automatically from the public source code on GitHub and lists the SHA-256 of Resound.exe, so you can check your download. Windows may still warn about it because it isn't code-signed yet: [Install Resound](install.md#windows-protected-your-pc).
