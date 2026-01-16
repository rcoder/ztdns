package ztdns

import "testing"

func TestExtractHostname(t *testing.T) {
	tests := []struct {
		qname    string
		zone     string
		expected string
	}{
		{"myhost.zt.example.com.", "zt.example.com.", "myhost"},
		{"myhost.zt.example.com", "zt.example.com", "myhost"},
		{"server.zt.example.com.", "zt.example.com.", "server"},
		{"zt.example.com.", "zt.example.com.", ""},
		{"other.domain.com.", "zt.example.com.", ""},
		{"sub.myhost.zt.example.com.", "zt.example.com.", ""}, // no subdomains
		{"MYHOST.zt.example.com.", "zt.example.com.", "MYHOST"},
	}

	for _, tt := range tests {
		t.Run(tt.qname, func(t *testing.T) {
			result := extractHostname(tt.qname, tt.zone)
			if result != tt.expected {
				t.Errorf("extractHostname(%q, %q) = %q, want %q", tt.qname, tt.zone, result, tt.expected)
			}
		})
	}
}
