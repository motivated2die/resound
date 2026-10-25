# Diagnostics and reporting a problem

Settings → **Diagnostics** collects what helps when something doesn't work.

| Section | What's there |
|---|---|
| Audio outputs | Which device each output plays to, its format and latency, or the error if it failed. Hover over a name that ends in “…” to read all of it. **Restart outputs** opens them again |
| Latency test | **Play test tone** and **Stop all**; the delay to each output appears below |
| Library index | How many sounds Resound knows, and whether it is still reading new ones |
| Phone connection | The UDP port (8080 by default), the app's address, paired devices |
| Files | **Open log** and **Copy log** (the whole log on the clipboard, ready to paste), **Data folder** and **Program folder** |

## Where Resound keeps its files

- The program: `%LOCALAPPDATA%\Programs\Resound`
- Settings, sound index, pairing, backups and the log: `%LOCALAPPDATA%\Resound`

## Report a problem

[Report a bug on GitHub](https://github.com/motivated2die/resound/issues/new?template=bug_report.yml). The form asks for:

1. your Resound version (Settings → App & startup) and Windows version;
2. what you did, what you expected and what happened instead;
3. the log: **Copy log** under Diagnostics and paste it, or attach the file from **Open log** (`debug.log`).

Ideas are welcome too: [suggest a feature](https://github.com/motivated2die/resound/issues/new?template=feature_request.yml).

:::warning Check the log before you post it
Issues on GitHub are public. The log never contains your keystrokes, pairing codes or keys. Resound leaves out your Windows user name (paths read `C:\Users\<user>\…`), your PC's name, the organisation in OneDrive folders and email addresses; **Copy log** removes them from older lines too. It still lists audio device names, folder and sound names and local network addresses. Replace anything private with `***` before uploading.
:::
