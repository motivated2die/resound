package server

import (
	"net"
	"slices"
	"strings"
)

// Address is a local IPv4 address phones can use to reach this PC.
type Address struct {
	Interface string
	IP        string
	// Label is a short, readable name for the adapter ("Wi-Fi", "WSL").
	Label string
	// Other is true for addresses a phone on the home network can usually
	// not reach: virtual adapters (WSL, Hyper-V, VMs, VPNs) and public IPs.
	Other bool
}

// virtualHints marks adapters that are usually not on the phone's network,
// with the label shown for them. Checked in order against the lower-cased
// adapter name; "vethernet" (Hyper-V's virtual switches) comes after "wsl",
// whose adapter is also a vEthernet one.
var virtualHints = []struct{ hint, label string }{
	{"wsl", "WSL"},
	{"default switch", "Hyper-V"},
	{"vethernet", "Hyper-V"},
	{"hyper-v", "Hyper-V"},
	{"virtualbox", "VirtualBox"},
	{"vmware", "VMware"},
	{"docker", "Docker"},
	{"tailscale", "Tailscale"},
	{"zerotier", "ZeroTier"},
	{"vpn", "VPN"},
	{"tap-", "VPN"},
	{"npcap", "Npcap"},
	{"bluetooth", "Bluetooth"},
	{"loopback", "Loopback"},
}

// describe labels an adapter and says whether it is an "other" one.
func describe(name string, private bool) (label string, other bool) {
	lower := strings.ToLower(name)
	for _, v := range virtualHints {
		if strings.Contains(lower, v.hint) {
			return v.label, true
		}
	}
	switch {
	case strings.Contains(lower, "wi-fi"), strings.Contains(lower, "wlan"), strings.Contains(lower, "wireless"):
		label = "Wi-Fi"
	case strings.Contains(lower, "ethernet"), strings.Contains(lower, "lan"):
		label = "Ethernet"
	default:
		label = name
	}
	return label, !private
}

// LocalAddresses lists the usable IPv4 addresses, most likely first:
// physical adapters with private (LAN) addresses before everything else.
func LocalAddresses() []Address {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	type ranked struct {
		Address
		score int
	}
	var out []ranked
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || ip.IsLinkLocalUnicast() || ip.IsLoopback() {
				continue
			}
			label, other := describe(ifc.Name, ip.IsPrivate())
			score := 0
			if ip.IsPrivate() {
				score += 2
			}
			if !other {
				score++
			}
			out = append(out, ranked{Address{Interface: ifc.Name, IP: ip.String(), Label: label, Other: other}, score})
		}
	}
	slices.SortStableFunc(out, func(a, b ranked) int { return b.score - a.score })
	res := make([]Address, len(out))
	for i, r := range out {
		res[i] = r.Address
	}
	return res
}
