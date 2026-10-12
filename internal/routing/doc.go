// Package routing configures and checks the Windows side of the virtual
// cable setup:
//
//   - detects VB-Cable ("CABLE Input" render / "CABLE Output" capture),
//   - mutes and unmutes the real microphone (IAudioEndpointVolume),
//   - switches "Listen to this device" on the microphone with "Playback
//     through this device" = CABLE Input, so the voice reaches the cable
//     together with the sounds Resound plays into it.
//
// The listen settings are undocumented endpoint properties that need
// administrator rights to write. The tray app therefore never writes them
// itself: it starts its own executable elevated with ApplyCommand, which
// performs the write, verifies it and exits.
package routing
