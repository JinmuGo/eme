package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

// decode helps tests read back the hooks structure from merged bytes.
func decodeSettings(t *testing.T, b []byte) map[string]json.RawMessage {
	t.Helper()
	root := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatalf("merged output is not valid JSON: %v\n%s", err, b)
	}
	return root
}

func hooksMap(t *testing.T, root map[string]json.RawMessage) map[string][]claudeHookGroup {
	t.Helper()
	hm := map[string][]claudeHookGroup{}
	if raw, ok := root["hooks"]; ok {
		if err := json.Unmarshal(raw, &hm); err != nil {
			t.Fatalf("hooks not decodable: %v", err)
		}
	}
	return hm
}

// TestMergeClaudeHooks_AddsAllThreeIntoEmptySettings: a fresh settings file gains the
// three eme events, each a command that stamps @eme_state.
func TestMergeClaudeHooks_AddsAllThreeIntoEmptySettings(t *testing.T) {
	out, added, err := mergeClaudeHooks(nil)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(added) != 3 {
		t.Fatalf("added = %v, want 3 events", added)
	}
	hm := hooksMap(t, decodeSettings(t, out))
	for _, ev := range []string{"UserPromptSubmit", "Notification", "Stop"} {
		groups := hm[ev]
		if !groupsHaveEme(groups) {
			t.Errorf("event %s missing an @eme_state command", ev)
		}
	}
	// The Stop hook stamps idle; spot-check the exact command shape.
	cmd := hm["Stop"][0].Hooks[0].Command
	if !strings.Contains(cmd, `@eme_state idle`) || !strings.Contains(cmd, `$TMUX_PANE`) {
		t.Errorf("Stop command = %q, want an @eme_state idle set-option on $TMUX_PANE", cmd)
	}
	if !strings.HasPrefix(cmd, `[ -n "$TMUX" ]`) || !strings.HasSuffix(cmd, `|| true`) {
		t.Errorf("Stop command must guard $TMUX and always exit 0, got %q", cmd)
	}
}

// TestMergeClaudeHooks_PreservesOtherKeysAndHooks is the load-bearing safety test: a
// real settings.json with unrelated keys AND a foreign SessionEnd hook (as cinch
// installs) must survive the merge untouched.
func TestMergeClaudeHooks_PreservesOtherKeysAndHooks(t *testing.T) {
	existing := []byte(`{
  "theme": "dark",
  "permissions": {"allow": ["Bash"]},
  "hooks": {
    "SessionEnd": [{"hooks": [{"type": "command", "command": "'/abs/cinch' agent-hook claude-session-end"}]}]
  }
}`)
	out, added, err := mergeClaudeHooks(existing)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(added) != 3 {
		t.Fatalf("added = %v, want 3", added)
	}
	root := decodeSettings(t, out)
	if _, ok := root["theme"]; !ok {
		t.Error("top-level 'theme' key was dropped")
	}
	if _, ok := root["permissions"]; !ok {
		t.Error("top-level 'permissions' key was dropped")
	}
	hm := hooksMap(t, root)
	se := hm["SessionEnd"]
	if len(se) != 1 || len(se[0].Hooks) != 1 ||
		se[0].Hooks[0].Command != "'/abs/cinch' agent-hook claude-session-end" {
		t.Errorf("cinch SessionEnd hook was not preserved verbatim: %+v", se)
	}
	if groupsHaveEme(se) {
		t.Error("eme must not touch the SessionEnd event")
	}
}

// TestMergeClaudeHooks_Idempotent: re-merging already-installed settings adds nothing
// and returns the input unchanged.
func TestMergeClaudeHooks_Idempotent(t *testing.T) {
	first, _, err := mergeClaudeHooks(nil)
	if err != nil {
		t.Fatalf("first merge: %v", err)
	}
	second, added, err := mergeClaudeHooks(first)
	if err != nil {
		t.Fatalf("second merge: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("re-install added %v, want nothing (idempotent)", added)
	}
	if string(second) != string(first) {
		t.Error("idempotent re-merge changed the bytes")
	}
}

// TestMergeClaudeHooks_AppendsBesideForeignHookOnSameEvent: if the user already has a
// non-eme hook on one of OUR events, eme appends its group rather than replacing.
func TestMergeClaudeHooks_AppendsBesideForeignHookOnSameEvent(t *testing.T) {
	existing := []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo mine"}]}]}}`)
	out, added, err := mergeClaudeHooks(existing)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	// Stop already existed (foreign), so it should still be counted as "added" (eme's
	// group is new) and the foreign one preserved.
	foundStop := false
	for _, a := range added {
		if a == "Stop" {
			foundStop = true
		}
	}
	if !foundStop {
		t.Fatalf("Stop should be in added=%v (eme's group is new)", added)
	}
	groups := hooksMap(t, decodeSettings(t, out))["Stop"]
	if len(groups) != 2 {
		t.Fatalf("Stop should have 2 groups (foreign + eme), got %d", len(groups))
	}
	if groups[0].Hooks[0].Command != "echo mine" {
		t.Errorf("foreign Stop hook not preserved as first group: %+v", groups[0])
	}
	if !groupsHaveEme(groups) {
		t.Error("eme's Stop group missing after append")
	}
}

// TestRemoveEmeHooks_StripsOnlyEme: uninstall removes eme's groups and the now-empty
// events, but leaves foreign hooks and keys intact.
func TestRemoveEmeHooks_StripsOnlyEme(t *testing.T) {
	// Start from a settings with cinch's SessionEnd + a foreign Stop, then install eme.
	base := []byte(`{"theme":"dark","hooks":{` +
		`"SessionEnd":[{"hooks":[{"type":"command","command":"cinch x"}]}],` +
		`"Stop":[{"hooks":[{"type":"command","command":"echo mine"}]}]}}`)
	installed, _, err := mergeClaudeHooks(base)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	cleaned, removed, err := removeEmeHooks(installed)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(removed) != 3 {
		t.Fatalf("removed = %v, want 3 events", removed)
	}
	root := decodeSettings(t, cleaned)
	if _, ok := root["theme"]; !ok {
		t.Error("theme dropped during uninstall")
	}
	hm := hooksMap(t, root)
	// UserPromptSubmit and Notification were eme-only → removed entirely.
	if _, ok := hm["UserPromptSubmit"]; ok {
		t.Error("empty UserPromptSubmit event should be deleted")
	}
	// Stop keeps the foreign group; SessionEnd untouched.
	if len(hm["Stop"]) != 1 || hm["Stop"][0].Hooks[0].Command != "echo mine" {
		t.Errorf("foreign Stop hook not preserved: %+v", hm["Stop"])
	}
	if len(hm["SessionEnd"]) != 1 || hm["SessionEnd"][0].Hooks[0].Command != "cinch x" {
		t.Errorf("SessionEnd not preserved: %+v", hm["SessionEnd"])
	}
	if groupsHaveEme(hm["Stop"]) {
		t.Error("eme command still present in Stop after uninstall")
	}
}

// TestRemoveEmeHooks_NoEmeIsNoop: uninstalling when nothing is installed returns input
// unchanged and reports nothing removed.
func TestRemoveEmeHooks_NoEmeIsNoop(t *testing.T) {
	existing := []byte(`{"hooks":{"SessionEnd":[{"hooks":[{"type":"command","command":"cinch x"}]}]}}`)
	out, removed, err := removeEmeHooks(existing)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}
	if string(out) != string(existing) {
		t.Error("no-op uninstall changed the bytes")
	}
}

// TestMergeClaudeHooks_RejectsInvalidJSON surfaces a clear error rather than corrupting.
func TestMergeClaudeHooks_RejectsInvalidJSON(t *testing.T) {
	if _, _, err := mergeClaudeHooks([]byte(`{not json`)); err == nil {
		t.Fatal("expected an error for invalid JSON input")
	}
}

// TestMergeClaudeHooks_NullHooksDoesNotPanic guards the nil-map edge: a "hooks": null
// (or a whole-file null) must merge cleanly, not panic on a nil-map assignment.
func TestMergeClaudeHooks_NullHooksDoesNotPanic(t *testing.T) {
	for _, in := range [][]byte{[]byte(`{"hooks": null}`), []byte(`null`), []byte(`{}`)} {
		out, added, err := mergeClaudeHooks(in)
		if err != nil {
			t.Fatalf("merge(%s): %v", in, err)
		}
		if len(added) != 3 {
			t.Errorf("merge(%s) added %v, want 3", in, added)
		}
		if !groupsHaveEme(hooksMap(t, decodeSettings(t, out))["Stop"]) {
			t.Errorf("merge(%s) did not install the Stop hook", in)
		}
	}
	// Uninstall on null hooks is a clean no-op.
	if out, removed, err := removeEmeHooks([]byte(`{"hooks": null}`)); err != nil || len(removed) != 0 || string(out) != `{"hooks": null}` {
		t.Errorf("removeEmeHooks(null hooks) = (%s, %v, %v), want no-op", out, removed, err)
	}
}

// TestMergeClaudeHooks_PreservesUnknownFieldsOnOurEvents guards the data-loss fix: a
// foreign hook that sits on one of eme's OWN events (Stop) and carries an extra key
// (timeout) must keep that key through BOTH install and uninstall — foreign groups
// pass through as raw bytes, never re-serialized from a lossy typed shape.
func TestMergeClaudeHooks_PreservesUnknownFieldsOnOurEvents(t *testing.T) {
	existing := []byte(`{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"my-formatter","timeout":30}]}]}}`)
	installed, _, err := mergeClaudeHooks(existing)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !strings.Contains(string(installed), `"timeout"`) {
		t.Errorf("install dropped the foreign hook's timeout field:\n%s", installed)
	}
	cleaned, _, err := removeEmeHooks(installed)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(string(cleaned), `"timeout"`) || !strings.Contains(string(cleaned), `"my-formatter"`) {
		t.Errorf("uninstall dropped the foreign hook (timeout/command):\n%s", cleaned)
	}
}

// TestEmeHookRecognition_IgnoresForeignMentionOfMarker guards the false-positive fix:
// a foreign command that merely MENTIONS @eme_state (but is not a set-option) must not
// be mistaken for eme's — install appends eme's hook beside it, uninstall never strips it.
func TestEmeHookRecognition_IgnoresForeignMentionOfMarker(t *testing.T) {
	existing := []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo checking @eme_state value"}]}]}}`)
	installed, added, err := mergeClaudeHooks(existing)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	foundStop := false
	for _, a := range added {
		if a == "Stop" {
			foundStop = true
		}
	}
	if !foundStop {
		t.Errorf("Stop not added — a foreign @eme_state mention was mistaken for eme's: %v", added)
	}
	if n := len(hooksMap(t, decodeSettings(t, installed))["Stop"]); n != 2 {
		t.Fatalf("Stop should have 2 groups (foreign echo + eme), got %d", n)
	}
	cleaned, _, err := removeEmeHooks(installed)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(string(cleaned), "echo checking @eme_state value") {
		t.Errorf("uninstall stripped the foreign @eme_state mention:\n%s", cleaned)
	}
}
