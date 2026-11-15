//go:build windows

package display

import "testing"

func TestBuildDisplayIDAndSignature(t *testing.T) {
	id := buildDisplayID(`\\?\DISPLAY#BOE1600#5&614b76b&0&UID4356#{e6f07b5f-ee97-4a90-b076-33f57bf4eaa7}`, 0x09E5, 0x1600, 4356)
	if id != "BOE1600:4356" {
		t.Fatalf("got %q, want BOE1600:4356", id)
	}

	fallback := buildDisplayID("", 0x09E5, 0x1600, 4354)
	if fallback != "09E51600:4354" {
		t.Fatalf("got %q, want 09E51600:4354", fallback)
	}

	sig := BuildSignature([]DisplayInfo{
		{ID: "SHP1517:8388688"},
		{ID: "BOE1600:4356"},
	})
	if sig != "BOE1600:4356|SHP1517:8388688" {
		t.Fatalf("unexpected signature: %q", sig)
	}
}

func TestQueryActiveLive(t *testing.T) {
	snap, err := QueryActive()
	if err != nil {
		t.Fatalf("QueryActive failed: %v", err)
	}
	if len(snap.Displays) == 0 {
		t.Fatal("expected at least 1 active display")
	}
	if snap.Signature == "" {
		t.Fatal("expected non-empty signature")
	}
}
