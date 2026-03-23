package app

import "testing"

func TestHello(t *testing.T) {
	h := New().Hello()
	if h.ID != PluginID {
		t.Fatalf("id: got %q want %q", h.ID, PluginID)
	}
}
