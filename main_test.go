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
	nameToID["yakuzakiwami3darkties"] = "1464821189921996860"
	manualMappings["YakuzaKiwami3"] = "1464821189921996860"

	got := resolveClientID("Balatro")
	if got != "1209665818464358430" {
		t.Errorf("resolveClientID(Balatro) = %q, want 1209665818464358430", got)
	}

	// manual mapping takes precedence and resolves a folder name that wouldn't normalize-match
	got = resolveClientID("YakuzaKiwami3")
	if got != "1464821189921996860" {
		t.Errorf("resolveClientID(YakuzaKiwami3) = %q, want 1464821189921996860", got)
	}

	got = resolveClientID("NonExistentGame")
	if got != "000000000000000000" {
		t.Errorf("resolveClientID(NonExistentGame) = %q, want 000000000000000000", got)
	}
}

func TestIsIgnoredGame(t *testing.T) {
	ignoredGames["SomeExactName"] = true
	defer delete(ignoredGames, "SomeExactName")

	tests := []struct {
		name string
		want bool
	}{
		{"SteamLinuxRuntime", true},
		{"SteamLinuxRuntime_soldier", true},
		{"SteamLinuxRuntime_sniper", true},
		{"SteamLinuxRuntime_4", true},
		{"SteamLinuxRuntime_99", true},
		{"Proton - Experimental", true},
		{"Proton 9.0", true},
		{"Proton Hotfix", true},
		{"SomeExactName", true},
		{"YakuzaKiwami3", false},
		{"Balatro", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isIgnoredGame(tt.name); got != tt.want {
				t.Errorf("isIgnoredGame(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestVersionIsSet(t *testing.T) {
	if version == "" {
		t.Error("version should not be empty")
	}
}

func TestMatchWindowsExecutable(t *testing.T) {
	populateMap([]DetectableApp{
		{ID: "356875762940379136", Name: "World of Warcraft", Executables: []Executable{
			{Name: "_retail_/wow.exe", OS: "win32"},
			{Name: "world of warcraft.app", OS: "darwin"},
		}},
		{ID: "1553234742529364018", Name: "World of Warcraft: Forever", Executables: []Executable{
			{Name: "wowb.exe", OS: "win32"},
		}},
		{ID: "111111111111111111", Name: "Black Souls", Executables: []Executable{
			{Name: "black souls/game.exe", OS: "win32"},
		}},
	})

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"battle.net windows path",
			`C:\Program Files (x86)\World of Warcraft\_classic_beta_\WowB.exe`,
			"World of Warcraft: Forever",
		},
		{
			"suffix with directory component",
			`C:\Program Files (x86)\World of Warcraft\_retail_\Wow.exe`,
			"World of Warcraft",
		},
		{
			"directory component must match",
			`C:\Games\Something\_ptr_\Wow.exe`,
			"",
		},
		{
			"generic basename needs its directory",
			`Z:\home\user\Games\black souls\Game.exe`,
			"Black Souls",
		},
		{
			"generic basename alone does not match",
			`Z:\home\user\Games\Unrelated\Game.exe`,
			"",
		},
		{
			"darwin executables are not indexed",
			"/Applications/World of Warcraft.app",
			"",
		},
		{
			"not an executable",
			"-launcherlogin",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchWindowsExecutable(tt.input)
			if got != tt.want {
				t.Errorf("matchWindowsExecutable(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
