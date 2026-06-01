package streams

import (
	"context"
	"strings"

	dbus "github.com/godbus/dbus/v5"
)

const mprisPrefix = "org.mpris.MediaPlayer2."

// queryMPRIS enumerates active MPRIS players on the session bus,
// resolves each player's OS PID, and fetches current track metadata.
// Returns nil (not an error) when the session bus is unavailable.
func queryMPRIS(ctx context.Context) ([]mprisPlayer, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, nil // DBus not available (headless, non-graphical session, etc.)
	}
	defer conn.Close()

	// List all names on the session bus.
	var names []string
	call := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0)
	if err := call.Store(&names); err != nil {
		return nil, err
	}

	var players []mprisPlayer
	for _, name := range names {
		if !strings.HasPrefix(name, mprisPrefix) {
			continue
		}
		playerName := strings.TrimPrefix(name, mprisPrefix)

		// Resolve the owner's OS PID via the DBus daemon.
		var pid uint32
		pidCall := conn.BusObject().CallWithContext(ctx,
			"org.freedesktop.DBus.GetConnectionUnixProcessID", 0, name)
		if err := pidCall.Store(&pid); err != nil {
			continue
		}

		p := mprisPlayer{Name: playerName, PID: pid}

		// Fetch current track metadata (best-effort).
		obj := conn.Object(name, "/org/mpris/MediaPlayer2")
		var variant dbus.Variant
		metaCall := obj.CallWithContext(ctx,
			"org.freedesktop.DBus.Properties.Get", 0,
			"org.mpris.MediaPlayer2.Player", "Metadata")
		if metaCall.Store(&variant) == nil {
			if meta, ok := variant.Value().(map[string]dbus.Variant); ok {
				p.Track = dbusStr(meta, "xesam:title")
				p.Artist = dbusStrFirst(meta, "xesam:artist")
			}
		}

		players = append(players, p)
	}
	return players, nil
}

// dbusStr extracts a string value from a Variant map.
func dbusStr(m map[string]dbus.Variant, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.Value().(string)
	return s
}

// dbusStrFirst extracts the first element from a string-or-[]string Variant.
// xesam:artist is a list, but single-artist tracks may encode it as a string.
func dbusStrFirst(m map[string]dbus.Variant, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch val := v.Value().(type) {
	case []string:
		if len(val) > 0 {
			return val[0]
		}
	case string:
		return val
	}
	return ""
}
