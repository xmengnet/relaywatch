package auth

import "testing"

func TestNormalizeIssuer(t *testing.T) {
	cases := map[string]string{
		"https://door.example/":      "https://door.example",
		"  https://door.example/  ":  "https://door.example",
		"https://door.example///":    "https://door.example",
		"https://door.example":       "https://door.example",
		"https://door.example/auth/": "https://door.example/auth",
		"/":                          "",
	}
	for in, want := range cases {
		if got := normalizeIssuer(in); got != want {
			t.Errorf("normalizeIssuer(%q) = %q, want %q", in, got, want)
		}
	}
}
