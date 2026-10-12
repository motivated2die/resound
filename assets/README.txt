icon.ico       the exe, Start Menu, taskbar and tray icon (used by go-winres).
               Generated from resound.png by go run ./scripts/icons, with
               every size Windows uses; don't edit it by hand.
icon-<n>.png   optional hand-drawn small sizes (16, 20, 24, 30, 32), exactly
               n x n pixels; the icon tool uses them instead of scaling.
resound.png  the logo in the panel's header, embedded by assets.go; also the
               phone remote's 512 px icon.
resound-192.png  the phone remote's 192 px icon.
               After changing either: go run ./scripts/icons
