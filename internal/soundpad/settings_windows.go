package soundpad

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

const mainKey = `Software\Leppsoft\Soundpad\MainFrame`

// ReadSettings reads Soundpad's keys from the registry, and its recent
// lists. The keys are opened for reading only.
func ReadSettings() (s Settings, recent []string) {
	main, err := registry.OpenKey(registry.CURRENT_USER, mainKey, registry.QUERY_VALUE)
	if err != nil {
		return Settings{}, nil
	}
	defer main.Close()
	value := func(k registry.Key) func(string) (uint32, bool) {
		return func(name string) (uint32, bool) {
			v, _, err := k.GetIntegerValue(name)
			return uint32(v), err == nil
		}
	}
	none := func(string) (uint32, bool) { return 0, false }
	auto := none
	if ak, err := registry.OpenKey(registry.CURRENT_USER, mainKey+`\AutoKeys`, registry.QUERY_VALUE); err == nil {
		defer ak.Close()
		auto = value(ak)
	}
	s = parseSettings(value(main), auto)
	if list, _, err := main.GetStringValue("recentSoundlists"); err == nil && len(list) < 64<<10 {
		recent = strings.Split(list, "|")
	}
	return s, recent
}
