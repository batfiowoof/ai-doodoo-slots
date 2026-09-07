package ws

import "testing"

func TestValidEmoteID(t *testing.T) {
	cases := map[string]bool{
		"gg":                     true,
		"fire_99":                true,
		"a":                      true,
		"":                       false,
		"UPPER":                  false,
		"has-dash":               false,
		"has space":              false,
		":gg:":                   false,
		string(make([]byte, 33)): false,
	}
	for id, want := range cases {
		if got := validEmoteID(id); got != want {
			t.Errorf("validEmoteID(%q) = %v, want %v", id, got, want)
		}
	}
}
