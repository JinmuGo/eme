// Package theme is the single source of truth for eme's color tokens.
//
// Colors are named by role, never by appearance, and defined here once so draw
// sites never inline a literal hex. The three load-bearing hues (Beacon, Working,
// Danger) are pinned per color depth with CompleteColor so termenv's nearest-match
// never muddies them; every role is light/dark adaptive so eme defers to the
// user's terminal theme. See DESIGN.md for the rationale and contrast data.
//
// NO_COLOR / CLICOLOR are honored automatically by the lipgloss/termenv renderer;
// every status also carries a glyph + weight + label so it reads with color off.
package theme

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ApplyBackground honors an explicit EME_THEME=light|dark override for light/dark
// adaptive color resolution, then returns. Call once at startup, before any TUI
// renders.
//
// Why an override is needed: eme runs inside a tmux display-popup, where the
// terminal cannot answer the OSC 11 background query (termenv skips it whenever
// TERM starts with tmux/screen), so lipgloss auto-detection falls back to a black
// background and resolves every adaptive role to its Dark variant. On a light
// terminal that yields the low-contrast beacon DESIGN.md §6 warns about. This env
// knob — a sibling of EME_BEACON_COLOR — is the reliable way for light-terminal
// users to opt in. Unset leaves lipgloss auto-detection in place (which still
// honors COLORFGBG and, outside tmux, the live OSC query).
func ApplyBackground() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EME_THEME"))) {
	case "light":
		lipgloss.SetHasDarkBackground(false)
	case "dark":
		lipgloss.SetHasDarkBackground(true)
	}
}

// Semantic color roles. Typed as TerminalColor so Beacon can be swapped for a
// plain override color at init without changing its consumers.
var (
	// Beacon — the one agent waiting for input; the header tally. Reserved:
	// nothing else on screen is ever this color. Overridable via EME_BEACON_COLOR.
	Beacon lipgloss.TerminalColor = resolveBeacon()

	// Working — an agent that is busy and does NOT need you. Steel-blue, not green.
	Working lipgloss.TerminalColor = lipgloss.CompleteAdaptiveColor{
		Dark:  lipgloss.CompleteColor{TrueColor: "#5E86A8", ANSI256: "67", ANSI: "4"},
		Light: lipgloss.CompleteColor{TrueColor: "#3E6488", ANSI256: "24", ANSI: "4"},
	}

	// Danger — crash / non-zero exit, kill-confirm, diff deletions. Cooler and
	// rarer than amber so it never out-shouts the beacon.
	Danger lipgloss.TerminalColor = lipgloss.CompleteAdaptiveColor{
		Dark:  lipgloss.CompleteColor{TrueColor: "#D55E00", ANSI256: "166", ANSI: "1"},
		Light: lipgloss.CompleteColor{TrueColor: "#B34700", ANSI256: "130", ANSI: "1"},
	}

	// Neutrals — inherit the terminal where they can.
	Text    lipgloss.TerminalColor = lipgloss.AdaptiveColor{Dark: "#D7DBE0", Light: "#1C1E22"} // names, titles, baseline
	Muted   lipgloss.TerminalColor = lipgloss.AdaptiveColor{Dark: "#7C8693", Light: "#5A626C"} // branches, help, rhyme, chrome
	Exited  lipgloss.TerminalColor = lipgloss.AdaptiveColor{Dark: "#98A0AB", Light: "#6B7280"} // clean finished agent
	Idle    lipgloss.TerminalColor = lipgloss.AdaptiveColor{Dark: "#5A626C", Light: "#80868F"} // never ran; recedes hardest
	Surface lipgloss.TerminalColor = lipgloss.AdaptiveColor{Dark: "#181C22", Light: "#ECECEA"} // selected-row lift (a platform, not a hue)
	Border  lipgloss.TerminalColor = lipgloss.AdaptiveColor{Dark: "#2A313A", Light: "#D3D6DB"} // panel border + rule
)

// defaultBeacon is the functional, colorblind-safe attention amber (Okabe-Ito).
var defaultBeacon = lipgloss.CompleteAdaptiveColor{
	Dark:  lipgloss.CompleteColor{TrueColor: "#E69F00", ANSI256: "214", ANSI: "11"},
	Light: lipgloss.CompleteColor{TrueColor: "#B45309", ANSI256: "130", ANSI: "3"},
}

// resolveBeacon honors EME_BEACON_COLOR, a retunable default for the one hue eme
// imposes. An override applies to every depth (a plain color); unset uses the
// pinned, light/dark-adaptive amber.
func resolveBeacon() lipgloss.TerminalColor {
	if v := os.Getenv("EME_BEACON_COLOR"); v != "" {
		return lipgloss.Color(v)
	}
	return defaultBeacon
}
