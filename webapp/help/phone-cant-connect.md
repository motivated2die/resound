# The phone can't connect

The app shows “Can't reach your PC” and keeps trying. Your PC simply doesn't answer a device it can't trust or reach, so the app can't tell which of these it is. Check them in order:

- [ ] **Resound is running on the PC.** The PC is awake and Resound's tray icon is there. Its **Phone remote** page shows a QR code.
- [ ] **The phone is on the same Wi-Fi as the PC.** Not mobile data, not a guest network, and no VPN on the phone or the PC. Guest networks and some routers keep devices apart (“client isolation”).
- [ ] **Scan the QR code again.** After a router restart the PC may have a new address; scanning again updates it. A pairing code works once and for 10 minutes.
- [ ] **Allow phones through the firewall.** On the PC's Phone remote page, click **Allow phones through the firewall** and approve the Windows prompt.
- [ ] **Windows treats your network as private.** Windows Settings → Network & internet → your Wi-Fi or Ethernet → **Network profile type: Private**. Resound's firewall rule covers private networks, not public ones.
- [ ] **No other firewall blocks Resound.** Antivirus suites with their own firewall need to allow Resound.exe (UDP port 8080).
- [ ] **The browser may reach your local network.** If the browser asks to allow access to devices on your local network, allow it.

## On iPhone and iPad

The installed app and Safari keep separate pairings. If the app from your home screen asks to connect, pair inside it: **Scan QR code** in the app.

## “Phones can't connect right now”

The PC's Phone remote page says this when Resound can't open its network port, usually because another program uses UDP port 8080. Click **Try again**, or close the other program. As a last resort, quit Resound, set another `serverPort` in `%LOCALAPPDATA%\Resound\config.json`, start Resound and scan its code again.

## “No network connection found”

The PC has no network connection Resound can use. Connect it to your Wi-Fi or router.
