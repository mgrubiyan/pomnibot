package main

import "testing"

func TestDotEnvValue(t *testing.T) {
	for in, want := range map[string]string{
		"GigaChat-3-Ultra":         "GigaChat-3-Ultra",
		" GigaChat-3-Ultra # main": "GigaChat-3-Ultra",
		`"secret with spaces"`:     "secret with spaces",
		`'value' # comment`:        "value",
		`"ends with quote'"`:       "ends with quote'",
		"abc#def":                  "abc#def",
		"":                         "",
		`"unterminated`:            `"unterminated`,
	} {
		if got := dotEnvValue(in); got != want {
			t.Errorf("dotEnvValue(%q) = %q, want %q", in, got, want)
		}
	}
}
