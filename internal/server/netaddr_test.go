package server

import "testing"

func TestDescribeAdapters(t *testing.T) {
	for _, tc := range []struct {
		name    string
		private bool
		label   string
		other   bool
	}{
		{"Ethernet", true, "Ethernet", false},
		{"Ethernet 2", true, "Ethernet", false},
		{"WLAN", true, "Wi-Fi", false},
		{"Wi-Fi", true, "Wi-Fi", false},
		{"vEthernet (Default Switch)", true, "Hyper-V", true},
		{"vEthernet (WSL (Hyper-V firewall))", true, "WSL", true},
		{"VirtualBox Host-Only Network", true, "VirtualBox", true},
		{"Tailscale", false, "Tailscale", true},
		{"Ethernet", false, "Ethernet", true}, // public address
	} {
		label, other := describe(tc.name, tc.private)
		if label != tc.label || other != tc.other {
			t.Errorf("describe(%q) = %q, %v; want %q, %v", tc.name, label, other, tc.label, tc.other)
		}
	}
}
