package streams

import (
	"testing"

	dbus "github.com/godbus/dbus/v5"
)

func TestDBusStrExtractsStringVariant(t *testing.T) {
	meta := map[string]dbus.Variant{
		"xesam:title":  dbus.MakeVariant("Forty Six & 2"),
		"mpris:length": dbus.MakeVariant(int64(365000000)),
	}

	if got := dbusStr(meta, "xesam:title"); got != "Forty Six & 2" {
		t.Fatalf("dbusStr() = %q, want title", got)
	}
	if got := dbusStr(meta, "mpris:length"); got != "" {
		t.Fatalf("dbusStr() for non-string = %q, want empty", got)
	}
	if got := dbusStr(meta, "missing"); got != "" {
		t.Fatalf("dbusStr() for missing key = %q, want empty", got)
	}
}

func TestDBusStrFirstAcceptsStringOrStringSlice(t *testing.T) {
	meta := map[string]dbus.Variant{
		"album_artist": dbus.MakeVariant("A Perfect Circle"),
		"artist":       dbus.MakeVariant([]string{"TOOL", "Maynard James Keenan"}),
		"empty_artist": dbus.MakeVariant([]string{}),
	}

	if got := dbusStrFirst(meta, "album_artist"); got != "A Perfect Circle" {
		t.Fatalf("dbusStrFirst() string = %q, want album artist", got)
	}
	if got := dbusStrFirst(meta, "artist"); got != "TOOL" {
		t.Fatalf("dbusStrFirst() slice = %q, want first artist", got)
	}
	if got := dbusStrFirst(meta, "empty_artist"); got != "" {
		t.Fatalf("dbusStrFirst() empty slice = %q, want empty", got)
	}
	if got := dbusStrFirst(meta, "missing"); got != "" {
		t.Fatalf("dbusStrFirst() missing key = %q, want empty", got)
	}
}
