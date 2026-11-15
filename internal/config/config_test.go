package config

import (
	"path/filepath"
	"testing"

	"external-display-position/internal/display"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(cfg.Profiles) != 0 {
		t.Fatalf("expected 0 profiles, got %d", len(cfg.Profiles))
	}
}

func TestSaveLoadUpsertAndDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	snap := &display.Snapshot{
		Signature: "BOE1600:4356|SHP1517:8388688",
		Displays: []display.DisplayInfo{
			{ID: "BOE1600:4356", X: 3840, Y: 798, Width: 2560, Height: 1600},
			{ID: "SHP1517:8388688", X: 0, Y: 0, Width: 3840, Height: 2400},
		},
	}

	prof := cfg.UpsertSnapshot("left-port", snap)
	if prof.Name != "left-port" {
		t.Fatalf("unexpected name: %q", prof.Name)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	got, ok := loaded.Profiles[snap.Signature]
	if !ok {
		t.Fatalf("missing saved signature %q", snap.Signature)
	}
	if got.Positions["BOE1600:4356"].X != 3840 || got.Positions["BOE1600:4356"].Y != 798 {
		t.Fatalf("unexpected positions: %+v", got.Positions)
	}

	// Updating without a name preserves the existing name.
	snap.Displays[0].X = 3900
	loaded.UpsertSnapshot("", snap)
	if loaded.Profiles[snap.Signature].Name != "left-port" {
		t.Fatalf("expected existing name preserved, got %q", loaded.Profiles[snap.Signature].Name)
	}

	sig, deleted := loaded.Delete("LEFT-PORT")
	if !deleted || sig != snap.Signature {
		t.Fatalf("expected delete by case-insensitive name, got deleted=%v sig=%q", deleted, sig)
	}
}
