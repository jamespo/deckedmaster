package main

import (
	"path/filepath"
	"testing"

	"github.com/mitchellh/go-homedir"
)

func TestExpandTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	homedir.Reset()
	t.Cleanup(homedir.Reset)

	tests := []struct {
		in   string
		want string
	}{
		{"~", home},
		{"~/bin/sonos-deck", filepath.Join(home, "bin/sonos-deck")},
		{"~~", "~~"},
		{"~user/bin", "~user/bin"},
		{"favourite-scroll", "favourite-scroll"},
		{"+1", "+1"},
		{"/usr/bin/env", "/usr/bin/env"},
		{"", ""},
	}

	for _, tc := range tests {
		if got := expandTilde(tc.in); got != tc.want {
			t.Errorf("expandTilde(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
