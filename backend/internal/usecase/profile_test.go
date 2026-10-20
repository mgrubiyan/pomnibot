package usecase

import (
	"strings"
	"testing"
)

func TestProfileNames(t *testing.T) {
	for _, value := range []string{"", "  ", "/start", strings.Repeat("я", 101)} {
		if _, err := CleanProfileName(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	for _, value := range []string{"Иван", "李", "Jean Luc", "О’Нил", strings.Repeat("я", 100)} {
		if got, err := CleanProfileName(" " + value + " "); err != nil || got != value {
			t.Errorf("%q: %q %v", value, got, err)
		}
	}
}
