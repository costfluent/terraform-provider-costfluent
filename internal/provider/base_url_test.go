package provider

import "testing"

func TestCheckBaseURL(t *testing.T) {
	for url, ok := range map[string]bool{
		"https://api.costfluent.com":   true,
		"https://api.costfluent.com/":  true,
		"http://localhost:5100":        true,
		"http://127.0.0.1:5100":        true,
		"http://[::1]:5100":            true,
		"http://api.costfluent.com":    false,
		"http://localhost.example.com": false,
		"http://127.0.0.1.nip.io":      false,
		"ftp://localhost":              false,
		"api.costfluent.com":           false,
		"https://":                     false,
		"://bad":                       false,
	} {
		if err := checkBaseURL(url); (err == nil) != ok {
			t.Errorf("checkBaseURL(%q) = %v, want ok %v", url, err, ok)
		}
	}
}
