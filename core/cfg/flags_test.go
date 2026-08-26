package cfg

import "testing"

func TestNewAppUsesAkaveFSName(t *testing.T) {
	if got := NewApp().Name; got != "akavefs" {
		t.Fatalf("CLI name = %q, want %q", got, "akavefs")
	}
}
