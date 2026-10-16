# Remove VB-Cable and Windows changes

Uninstalling Resound leaves VB-Cable and two Windows settings in place, since other programs may rely on them. To undo everything, follow these steps. Each one stands on its own.

## Switch your apps back to your microphone

In Discord (User Settings → Voice & Video → **Input Device**) and in your games, choose your headset or microphone instead of CABLE Output. Otherwise they lose your voice once VB-Cable is gone.

## Turn off Listen to this device

While Resound is still installed: Settings → Audio & voice chat → **Stop routing microphone into the cable**. Or in Windows:

1. Press <kbd>Win</kbd> + <kbd>R</kbd>, type `mmsys.cpl` and press <kbd>Enter</kbd>.
2. On the **Recording** tab, double-click your microphone.
3. On the **Listen** tab, turn off **Listen to this device** and click **OK**.

## Remove VB-Cable

1. Download the VB-Cable package from [vb-audio.com/Cable](https://vb-audio.com/Cable/) again if you don't have it, and unzip it.
2. Right-click `VBCABLE_Setup_x64.exe` → **Run as administrator** → **Remove Driver**.
3. Restart your PC.

Afterwards, check your default devices: Windows Settings → System → **Sound** → Output and Input.

## Remove the firewall rule

1. Open **Windows Defender Firewall with Advanced Security** (search for “firewall” in the Start menu, then **Advanced settings**).
2. Under **Inbound Rules**, select the rules named **Resound** and click **Delete**.

Or, in PowerShell as administrator:

```powershell
Remove-NetFirewallRule -DisplayName "Resound"
```
