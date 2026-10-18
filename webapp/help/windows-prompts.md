# Windows prompts and warnings

Resound itself never runs as administrator. Windows asks for your approval only for the few changes that need it, and only when you start them.

| You see | When | Why |
|---|---|---|
| **Windows protected your PC** (SmartScreen) | Running the downloaded Resound.exe | Resound isn't code-signed yet. Choose **More info** → **Run anyway**. See [Install Resound](install.md#windows-protected-your-pc) |
| A UAC prompt from **VB-Audio Software** | **Install VB-Cable** | VB-Audio's driver setup needs administrator rights |
| **Install this device software?** | During the VB-Cable install | Windows asks before adding VB-Audio's driver. Choose **Install** |
| A UAC prompt for **Resound** | **Connect to VB-Cable** | Turning on Listen to this device for your microphone needs administrator rights, once |
| A UAC prompt for **Resound** | **Allow phones through the firewall** | Adding the firewall rule needs administrator rights |
| **Windows Defender Firewall has blocked some features** | First start | Choose **Allow** for private networks. If you closed it, use **Allow phones through the firewall** on the Remote App page, which also removes the block |
| The microphone icon in the taskbar | While the panel shows the audio flow | Resound listens to your microphone and CABLE Output to draw the level bars. Nothing is recorded or kept |
| **Restart your PC to finish** | After installing VB-Cable | VB-Cable is ready after a restart. Resound reopens afterwards |

Declining a prompt changes nothing; you can start the same step again later.

:::note Antivirus warnings
A new, unsigned program is sometimes flagged by antivirus software. Releases are built from the public source code on GitHub, and each release lists the SHA-256 of Resound.exe so you can check your download. If your antivirus flags it, please [report it](https://github.com/motivated2die/resound/issues).
:::
