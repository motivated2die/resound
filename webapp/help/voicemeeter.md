# Use Voicemeeter or another mixer

If you already route your audio with a virtual mixer such as Voicemeeter, it can combine your voice and your sounds instead of VB-Cable. This is a non-standard setup: Resound never reads or changes your mixer's settings, so you set the mixer up yourself, once.

## Before you install VB-Cable

Resound's usual setup plays your sounds into **CABLE Input** and turns on Windows' **Listen to this device** for your microphone, so both arrive at **CABLE Output**. Many Voicemeeter setups use VB-Cable too, for example **CABLE Output** as a Hardware Input or **CABLE Input** as output A2. Connecting Resound to such a cable adds your sounds and a second copy of your voice to it: echoes, doubled audio, or sounds in places you didn't expect.

To check, open Voicemeeter and look at the device names at the top of each **Hardware Input** strip and next to **A1**, **A2** and **A3** (top right):

- **No CABLE anywhere:** VB-Cable is free. Use Resound's usual setup ([Set up voice chat](voice-chat.md)), or Voicemeeter as below.
- **CABLE in one of them:** your mixer uses VB-Cable. Don't click **Install VB-Cable** or **Connect to VB-Cable**; set up Resound with Voicemeeter instead.

## Set up Voicemeeter Banana

:::viz voicemeeter
Your microphone is Voicemeeter's **Hardware Input 1**; Resound plays into **Voicemeeter AUX Input**. Both strips send to **B1**, and your apps use **Voicemeeter Out B1** as their microphone. You hear the sounds on your headphones straight from Resound, so **A1** stays off on the AUX strip.
:::

**In Voicemeeter:**

1. Click the name at the top of **Hardware Input 1** and choose your microphone (the **WDM** entry).
2. On that strip, turn on **B1**. Turn on **A1** only if you want to hear yourself.
3. On the **Voicemeeter AUX** strip (under Virtual Inputs), turn on **B1** and turn **A1** off. With A1 on, you hear every sound twice, slightly apart.
4. Keep Voicemeeter running. Menu → **Run on Windows Startup** starts it with Windows; while it is closed, Voicemeeter Out B1 stays silent.

**In Resound,** Settings → Audio & voice chat → **Advanced**:

1. **Sounds for others play into:** choose **Voicemeeter AUX Input**. A yellow **Non-standard setup** note appears below it.
2. **Others' apps listen to:** choose **Voicemeeter Out B1**.
3. **Your microphone** (at the top of the audio flow): choose the same microphone as Hardware Input 1, so Resound mutes it while sounds play.
4. **You hear:** your headphones, not one of Voicemeeter's inputs.

**In Discord and your games,** choose **Voicemeeter Out B1** as your microphone (input device): [Use Resound in Discord and games](apps.md).

:::tip Other mixers
Any mixer works the same way: Resound plays into one of its inputs, the mixer combines it with your microphone on one output, and your apps record that output. Choose the input and the output in Resound's Advanced section.
:::

## Check that it works

Open Settings → Audio & voice chat. The audio flow shows **Via your mixer** on your microphone's line and **Your mixer** on the line to **Others hear**.

1. Talk: the bar under your microphone moves, and in Voicemeeter the meters of Hardware Input 1 and B1.
2. Click **Test sound**: the soundboard's bar moves, in Voicemeeter the AUX strip's and B1's.
3. The bar under **Others hear** (Out B1) shows what your friends get: your voice and the sound. If it stays empty, Voicemeeter isn't running or a strip isn't sent to B1.

## Go back to VB-Cable

In Settings → Audio & voice chat → Advanced, click **Back to VB-Cable** (or choose **Automatic: VB-Cable** under Sounds for others play into). Resound forgets the mixer's output, and the usual setup returns. Your Voicemeeter settings stay as they are.
