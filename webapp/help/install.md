# Install Resound

Resound is one small program for Windows 10 and 11 (64-bit). It installs for your Windows user only and needs no administrator rights.

## Download and run

1. Download **Resound.exe** from [getresound.app](/) or the [latest release on GitHub](https://github.com/motivated2die/resound/releases/latest).
2. Run it. Resound installs itself into `%LOCALAPPDATA%\Programs\Resound`, adds a Start Menu entry and starts.
3. A notice in the corner of the screen says Resound is installed. Choose **Start setup** to begin the [guided setup](guided-setup.md).

## “Windows protected your PC”

Resound isn't code-signed yet, so Windows SmartScreen may warn about the new download. Choose **More info**, then **Run anyway**.

To make sure the file is the published one, compare its SHA-256 with the one in the release notes. In PowerShell, in your Downloads folder:

```powershell
(Get-FileHash .\Resound.exe -Algorithm SHA256).Hash
```

## Find Resound in the tray

Resound has no window on the taskbar. It runs in the background from its icon in the system tray, next to the clock. Click the icon to open the [panel](panel.md); right-click it for **Show panel** and **Quit**.

Windows usually hides new tray icons behind the **^** arrow (**Show hidden icons**). Pin Resound's icon so it stays in sight:

:::viz tray-pin
Click the **^** arrow next to the clock, then drag the Resound icon from the box that opens onto the taskbar and let go.
:::

1. Click the **^** arrow next to the clock.
2. Drag the Resound icon out of the box onto the taskbar, next to the arrow.

:::tip Prefer Settings?
Windows 11: Settings → Personalization → Taskbar → **Other system tray icons**, turn on **Resound**. Windows 10: Settings → Personalization → Taskbar → **Select which icons appear on the taskbar**.
:::

## Open Resound again

Closed the panel? Click the tray icon. Quit Resound, or can't find it in the tray? Start it from the Start menu:

:::viz start-menu
Press the <kbd>Windows</kbd> key or click **Start**, type **res** and open **Resound**. It starts in the tray; click its icon to open the panel. If Resound was still running, the panel opens right away.
:::

1. Press the <kbd>Windows</kbd> key and type **res**.
2. Choose **Resound** (or press <kbd>Enter</kbd>).
3. Click the Resound icon in the tray to open the panel.

## Start with Windows

Resound doesn't start with Windows unless you turn it on: in the last step of the guided setup, or under Settings → App & startup → **Start with Windows**. While Resound isn't running, your phone can't connect and sounds don't play.

## What you need

- Windows 10 or 11, 64-bit.
- For voice chat: VB-Audio Virtual Cable, free donationware by VB-Audio. Resound installs it for you, see [Set up voice chat](voice-chat.md).
- For the remote: a phone, tablet or second screen on the same network as the PC, with a current browser (Chrome, Edge, Safari, Firefox, Samsung Internet).

Already installed? Newer versions install the same way and keep your settings, see [Updates](updates.md).
