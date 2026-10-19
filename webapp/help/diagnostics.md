# Diagnostics and reporting a problem

Settings → **Diagnostics** collects what helps when something doesn't work.

| Section | What's there |
|---|---|
| Audio outputs | Which device each output plays to, its format and latency, or the error if it failed. **Restart outputs** opens them again |
| Latency test | **Play test tone** and **Stop all**; the delay to each output appears below. **Choose test sound…** tests with one of your own files |
| Library index | How many sounds Resound knows, and whether it is still reading new ones |
| Phone connection | The UDP port (8080 by default), the app's address, paired devices |
| Files | **Open log**, **Data folder** and **Open program folder** |

## Where Resound keeps its files

- The program: `%LOCALAPPDATA%\Programs\Resound`
- Settings, sound index, pairing, backups and the log: `%LOCALAPPDATA%\Resound`

## Report a problem

[Report a bug on GitHub](https://github.com/motivated2die/resound/issues/new?template=bug_report.yml). The form asks for:

1. your Resound version (Settings → App & startup) and Windows version;
2. what you did, what you expected and what happened instead;
3. the log: **Open log** under Diagnostics, attached as a file (`debug.log`).

Ideas are welcome too: [suggest a feature](https://github.com/motivated2die/resound/issues/new?template=feature_request.yml).

:::warning Check the log before you post it
Issues on GitHub are public. The log never contains your keystrokes, pairing codes or keys, but it lists device names, folder and sound names (with your Windows user name in their paths), your PC's name and local network addresses. Replace anything private with `***` before uploading.
:::
