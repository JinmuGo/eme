package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// calmViews is a populated dashboard with nothing waiting or crashed: two working
// agents and one idle. The all-calm tally and the working-spinner tests build on it.
func calmViews() []SessionView {
	return []SessionView{
		{DisplayName: "app", Root: "/app", Worktrees: []WorktreeView{
			{Name: "a", Branch: "feat/a", SessionID: "app", Status: StatusWorking},
			{Name: "b", Branch: "feat/b", SessionID: "app", Status: StatusWorking},
			{Name: "c", Branch: "main", SessionID: "app", IsMain: true, Status: StatusIdle},
		}},
	}
}

// TestHeaderDropsRhymeOnPopulated: the rhyme no longer renders on a populated header —
// brand spend stays off the cockpit (DESIGN §9). The wordmark still anchors it.
func TestHeaderDropsRhymeOnPopulated(t *testing.T) {
	v := NewDashboard(sampleViews(), nil).View()
	if strings.Contains(v, "eeny") {
		t.Errorf("populated header must not carry the rhyme\n%s", v)
	}
	if !strings.Contains(v, "eme") {
		t.Errorf("populated header should still show the wordmark\n%s", v)
	}
}

// TestFirstRunWelcome: the empty state is a real welcome — the wordmark, the relocated
// rhyme, a one-liner, and the single n action — not the old bare "No sessions" sentence.
func TestFirstRunWelcome(t *testing.T) {
	v := NewDashboard(nil, nil).View()
	for _, want := range []string{"eeny · meeny · miny · moe", "mission control", "Run agents across git worktrees", "n"} {
		if !strings.Contains(v, want) {
			t.Errorf("first-run welcome missing %q\n%s", want, v)
		}
	}
	if strings.Contains(v, "No sessions") {
		t.Errorf("first-run should be a welcome, not the old bare sentence\n%s", v)
	}
}

// TestTallyAllCalmWithRunning: a populated, nothing-waiting dashboard confirms it's live
// with a positive "all calm · N running" — not a blank corner that reads as broken.
func TestTallyAllCalmWithRunning(t *testing.T) {
	v := NewDashboard(calmViews(), nil).View()
	if !strings.Contains(v, "all calm · 2 running") {
		t.Errorf("all-calm tally should read 'all calm · 2 running'\n%s", v)
	}
}

// TestTallyAllCalmNoRunning: when every agent is idle (none running, waiting, or
// crashed), the tally is a plain "all calm" with no running count.
func TestTallyAllCalmNoRunning(t *testing.T) {
	views := []SessionView{{DisplayName: "app", Root: "/app", Worktrees: []WorktreeView{
		{Name: "a", SessionID: "app", Status: StatusIdle},
	}}}
	v := NewDashboard(views, nil).View()
	if !strings.Contains(v, "all calm") {
		t.Errorf("idle-only dashboard should read 'all calm'\n%s", v)
	}
	if strings.Contains(v, "running") {
		t.Errorf("no agent is running — tally should not mention running\n%s", v)
	}
}

// TestWorkingGlyphAnimates: a working row's glyph advances with the tick frame (motion =
// alive) while a waiting row stays a dead-still ● (DESIGN §5.1). Frame 0 is the canonical
// ◐; frame 1 is ◓; the static ● is present at both.
func TestWorkingGlyphAnimates(t *testing.T) {
	views := []SessionView{{DisplayName: "app", Root: "/app", Worktrees: []WorktreeView{
		{Name: "w", SessionID: "app", Status: StatusWorking},
		{Name: "b", SessionID: "app", Status: StatusWaiting},
	}}}
	m := NewDashboard(views, nil)

	v0 := m.View()
	if !strings.Contains(v0, "◐") || !strings.Contains(v0, "●") {
		t.Errorf("frame 0: want working ◐ and waiting ●\n%s", v0)
	}

	m.glyphFrame = 1
	v1 := m.View()
	if !strings.Contains(v1, "◓") {
		t.Errorf("frame 1: working glyph should advance to ◓\n%s", v1)
	}
	if strings.Contains(v1, "◐") {
		t.Errorf("frame 1: the static ◐ should have animated away\n%s", v1)
	}
	if !strings.Contains(v1, "●") {
		t.Errorf("frame 1: the waiting beacon ● must stay static\n%s", v1)
	}
}

// TestQuietWorkingGlyphFrozen: a gone-quiet working agent does NOT animate — stillness is
// the interim "this one may want you" hint, so it stays ◐ even as the frame advances.
func TestQuietWorkingGlyphFrozen(t *testing.T) {
	views := []SessionView{{DisplayName: "app", Root: "/app", Worktrees: []WorktreeView{
		{Name: "q", SessionID: "app", Status: StatusWorking, Quiet: true},
	}}}
	m := NewDashboard(views, nil)
	m.glyphFrame = 1
	v := m.View()
	if !strings.Contains(v, "◐") {
		t.Errorf("a quiet working agent should freeze at ◐, not animate\n%s", v)
	}
	if strings.ContainsAny(v, "◓◑◒") {
		t.Errorf("a quiet working agent must not show a spinner frame\n%s", v)
	}
}

// TestCaffeinateBadgeIsMutedNotWorking: the keep-awake badge renders in muted chrome, not
// the reserved working hue (DESIGN §3.3/§10). Asserted on a working-agent-free view, so
// the working hue would only appear if the badge wrongly wore it.
func TestCaffeinateBadgeIsMutedNotWorking(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	views := []SessionView{{DisplayName: "app", Root: "/app", Caffeinate: "auto", Worktrees: []WorktreeView{
		{Name: "a", SessionID: "app", Status: StatusIdle},
	}}}
	v := NewDashboard(views, nil).View()
	if !strings.Contains(v, "(caf~)") {
		t.Errorf("the auto caffeinate badge should render\n%s", v)
	}
	// working dark hue #5E86A8 = rgb(94,134,168); muted dark #7C8693 = rgb(124,134,147).
	if strings.Contains(v, "38;2;94;134;168") {
		t.Errorf("the caffeinate badge must not wear the reserved working hue\n%s", v)
	}
	if !strings.Contains(v, "38;2;124;134;147") {
		t.Errorf("the caffeinate badge should render in muted\n%s", v)
	}
}

// TestUnhookedNudgeAppears (ET2): when live agents run un-hooked, the footer shows a calm
// muted nudge to install hooks — the path that lights the real beacon. The two working agents
// in calmViews carry no Hooked flag, so the nudge counts both.
func TestUnhookedNudgeAppears(t *testing.T) {
	v := NewDashboard(calmViews(), nil).View()
	if !strings.Contains(v, "2 un-hooked · eme hooks install") {
		t.Errorf("expected the un-hooked nudge for 2 working agents\n%s", v)
	}
}

// TestHookedHidesNudge: when every working agent is hooked, there is nothing to nudge — the
// beacon path is already wired, so the footer stays clean.
func TestHookedHidesNudge(t *testing.T) {
	views := []SessionView{{DisplayName: "app", Root: "/app", Worktrees: []WorktreeView{
		{Name: "a", SessionID: "app", Status: StatusWorking, Hooked: true},
		{Name: "b", SessionID: "app", Status: StatusWorking, Hooked: true},
	}}}
	v := NewDashboard(views, nil).View()
	if strings.Contains(v, "un-hooked") {
		t.Errorf("all agents hooked — no nudge expected\n%s", v)
	}
}

// TestFirstRunHidesNudge: the empty first-run screen teaches via the welcome, so the nudge is
// suppressed there even with zero hooked agents.
func TestFirstRunHidesNudge(t *testing.T) {
	v := NewDashboard(nil, nil).View()
	if strings.Contains(v, "un-hooked") {
		t.Errorf("first run should not show the hook nudge\n%s", v)
	}
}
