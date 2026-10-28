package config

import "strings"

// cleanKeybinds tidies keybinds read from disk, so the app only ever meets
// the shapes it writes itself: empty combinations go; further push-to-talk
// combinations move up when the first one is empty; programs without a
// file name or listed twice go; an override with neither keys nor Off is
// no override; an overlay or launchpad override keeps one combination.
func (c *Config) cleanKeybinds() {
	clean := func(list [][]uint16) [][]uint16 {
		var out [][]uint16
		for _, combo := range list {
			if len(combo) > 0 {
				out = append(out, combo)
			}
		}
		return out
	}
	c.PauseHotkeys, c.StopHotkeys = clean(c.PauseHotkeys), clean(c.StopHotkeys)
	c.PushToTalkExtra = clean(c.PushToTalkExtra)
	if len(c.PushToTalkKeys) == 0 && len(c.PushToTalkExtra) > 0 {
		c.PushToTalkKeys, c.PushToTalkExtra = c.PushToTalkExtra[0], c.PushToTalkExtra[1:]
	}
	if len(c.PushToTalkExtra) == 0 {
		c.PushToTalkExtra = nil
	}
	seen := map[string]bool{}
	apps := c.AppKeybinds[:0]
	for _, a := range c.AppKeybinds {
		a.Exe = strings.ToLower(strings.TrimSpace(a.Exe))
		if a.Exe == "" || seen[a.Exe] {
			continue
		}
		seen[a.Exe] = true
		for _, o := range []**KeyOverride{&a.PushToTalk, &a.Pause, &a.Stop, &a.Overlay, &a.Launchpad} {
			if *o == nil {
				continue
			}
			if (*o).Keys = clean((*o).Keys); (*o).Off {
				(*o).Keys = nil
			} else if len((*o).Keys) == 0 {
				*o = nil
			}
		}
		for _, o := range []*KeyOverride{a.Overlay, a.Launchpad} {
			if o != nil && len(o.Keys) > 1 {
				o.Keys = o.Keys[:1]
			}
		}
		apps = append(apps, a)
	}
	c.AppKeybinds = apps
	if len(apps) == 0 {
		c.AppKeybinds = nil
	}
}
