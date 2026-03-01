package main

import "testing"

func TestNormalizeGameName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Balatro", "balatro"},
		{"Counter-Strike 2", "counterstrike2"},
		{"Ragnarök", "ragnarok"},
		{"The Witcher 3: Wild Hunt", "thewitcher3wildhunt"},
		{"DARK SOULS III", "darksoulsiii"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeGameName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeGameName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractSteamGameName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"native linux",
			"/home/user/.steam/steam/steamapps/common/Balatro/balatro",
			"Balatro",
		},
		{
			"flatpak",
			"/home/user/.var/app/com.valvesoftware.Steam/.steam/steam/steamapps/common/Balatro/balatro",
			"Balatro",
		},
		{
			"proton windows path",
			"C:\\steamapps\\common\\Celeste\\Celeste.exe",
			"Celeste",
		},
		{
			"no steamapps",
			"/usr/bin/firefox",
			"",
		},
		{
			"game folder only",
			"/steamapps/common/Factorio",
			"Factorio",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSteamGameName(tt.input)
			if got != tt.want {
				t.Errorf("extractSteamGameName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveClientID(t *testing.T) {
	// seed lookup map
	nameToID["balatro"] = "1209665818464358430"

	got := resolveClientID("Balatro")
	if got != "1209665818464358430" {
		t.Errorf("resolveClientID(Balatro) = %q, want 1209665818464358430", got)
	}

	got = resolveClientID("NonExistentGame")
	if got != "000000000000000000" {
		t.Errorf("resolveClientID(NonExistentGame) = %q, want 000000000000000000", got)
	}
}

func TestVersionIsSet(t *testing.T) {
	if version == "" {
		t.Error("version should not be empty")
	}
}
