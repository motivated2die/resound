# Set up voice chat

Discord and games listen to one microphone. To make your sounds heard there, Resound mixes them with your voice on a virtual microphone called **CABLE Output**, which comes from the free driver VB-Cable by VB-Audio.

:::viz audio-flow
Your voice reaches VB-Cable through Windows' **Listen to this device**; Resound plays your sounds into it too. Apps that use **CABLE Output** as their microphone get both. You hear the sounds on your own headphones, separately.
:::

You set this up once, in the guided setup's audio step or under Settings → **Audio & voice chat**. Three steps: install VB-Cable, connect it, choose it in your apps.

## Install VB-Cable

Click **Install VB-Cable**. Resound downloads VB-Audio's original package, checks its digital signature and runs VB-Audio's own setup.

- Windows asks for your approval (a UAC prompt naming VB-Audio). It may also ask “Install this device software?”: choose **Install**.
- By installing VB-Cable you accept VB-Audio's license. VB-Cable is donationware: if you like it, consider [supporting VB-Audio](https://vb-audio.com/Cable/).

If the automatic install fails, choose **Install manually**: it opens VB-Audio's website. Unzip the download, right-click `VBCABLE_Setup_x64.exe`, choose **Run as administrator** and click **Install Driver**. Then click **Check again** in Resound.

## Restart your PC

VB-Cable is ready after a restart. Resound shows “Restart your PC to finish” with a **Restart now** button: save your work first, then confirm. After you sign in again, Resound starts by itself and continues where you left off.

If the cable already shows up before the restart, Resound also offers **Not now**. VB-Audio still recommends restarting.

:::note Your speakers stay your speakers
Windows sometimes makes VB-Cable the default playback or recording device when it is installed, and suddenly you hear nothing. Resound puts your previous default devices back afterwards.
:::

## Connect your microphone

Click **Connect to VB-Cable**. Resound then:

- plays the sounds for others into **CABLE Input**, VB-Cable's other end;
- turns on Windows' **Listen to this device** for your microphone, playing through CABLE Input, so your voice is in the cable too.

Windows asks for administrator approval once. In the panel's audio flow, the **Not routed** marker disappears and the line from your microphone lights up while you talk.

## Choose CABLE Output in your apps

In Discord, your games and any other voice app, set the microphone (input device) to **CABLE Output (VB-Audio Virtual Cable)** instead of your headset. Your voice still comes through, because it now travels through the cable. How to do it in each app: [Use Resound in Discord and games](apps.md).

## Check that it works

In Settings → Audio & voice chat:

1. Talk: the bar under your microphone moves.
2. Click **Test sound**: the soundboard's bar moves, and you hear a short chime.
3. The bar under **Others hear** shows what your friends get: your voice and the chime.

To test in Discord, use **Let's Check** under Voice & Video while a sound plays. Windows' own Sound dialog is a poor test: its meters react late and only move while the dialog is open.

## Use your own mixer instead (Voicemeeter)

If you already route your audio with a virtual mixer such as Voicemeeter, you don't need VB-Cable: your mixer combines your voice and the sounds. Resound never reads or changes your mixer's settings; you set it up yourself.

1. In Resound, Settings → Audio & voice chat → **Advanced**: under **Sounds for others play into**, choose an input of your mixer, for example **Voicemeeter AUX Input**.
2. In your mixer, send your microphone and that input to the same output. In Voicemeeter Banana: choose your microphone as **Hardware Input 1**, turn on **B1** for it and for the AUX strip, and turn **A1** off on the AUX strip (Resound already plays the sounds to your headphones; with A1 on you'd hear them twice).
3. Back in Resound, under **Others' apps listen to**, choose that output, for example **Voicemeeter Out B1**. The audio flow's **Others hear** bar now shows what arrives there.
4. Under **Your microphone**, choose the microphone your mixer records, so muting it while sounds play works.
5. In Discord and your games, choose that output (**Voicemeeter Out B1**) as your microphone.

Keep your mixer running: while it's closed, its output stays silent. To go back, click **Back to VB-Cable** under Advanced.
