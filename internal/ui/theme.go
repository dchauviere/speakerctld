package ui

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// DefaultColor is the dominant color of the stylesheets.
const DefaultColor = "blue"

var namedColors = map[string]string{
	"blue":   "#2867d8",
	"green":  "#2a9d62",
	"red":    "#d83a3a",
	"orange": "#e07a1f",
	"purple": "#7a4fd8",
	"teal":   "#1b9aaa",
	"pink":   "#d84f9a",
	"gray":   "#5f6f86",
}

const defaultHex = "#2867d8"

var (
	themeMu sync.RWMutex
	theme   *strings.Replacer
)

// ParseColor converts a color name or hex value (#rgb, #rrggbb) to RGB components.
func ParseColor(value string) (r, g, b int, err error) {
	v := strings.ToLower(strings.TrimSpace(value))
	if hex, ok := namedColors[v]; ok {
		v = hex
	}
	v = strings.TrimPrefix(v, "#")
	if len(v) == 3 {
		v = string([]byte{v[0], v[0], v[1], v[1], v[2], v[2]})
	}
	if len(v) != 6 {
		return 0, 0, 0, fmt.Errorf("invalid color %q: use a name (blue, green, red, orange, purple, teal, pink, gray) or a hex value", value)
	}
	n, perr := strconv.ParseUint(v, 16, 32)
	if perr != nil {
		return 0, 0, 0, fmt.Errorf("invalid color %q: use a name (blue, green, red, orange, purple, teal, pink, gray) or a hex value", value)
	}
	return int(n >> 16), int(n >> 8 & 0xff), int(n & 0xff), nil
}

func mix(r, g, b int, factor float64) string {
	return fmt.Sprintf("#%02x%02x%02x", int(float64(r)*factor), int(float64(g)*factor), int(float64(b)*factor))
}

// SetThemeColor sets the dominant color used by the web interface stylesheets.
func SetThemeColor(value string) error {
	r, g, b, err := ParseColor(value)
	if err != nil {
		return err
	}
	rgb := fmt.Sprintf("%d, %d, %d", r, g, b)
	var replacer *strings.Replacer
	if rgb != "40, 103, 216" {
		replacer = strings.NewReplacer(
			"#2867d8", mix(r, g, b, 1),
			"#1d4fae", mix(r, g, b, 0.78),
			"#234d86", mix(r, g, b, 0.58),
			"#162b4c", mix(r, g, b, 0.3),
			"40, 103, 216", rgb,
			"52, 116, 221", rgb,
		)
	}
	themeMu.Lock()
	theme = replacer
	themeMu.Unlock()
	return nil
}

// ApplyTheme recolors a stylesheet with the configured dominant color.
func ApplyTheme(css []byte) []byte {
	themeMu.RLock()
	replacer := theme
	themeMu.RUnlock()
	if replacer == nil {
		return css
	}
	return []byte(replacer.Replace(string(css)))
}
