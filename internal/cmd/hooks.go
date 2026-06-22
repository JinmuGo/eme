package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jinmu/eme/internal/errors"
)

// Agent hooks let an agent PUSH its real state into the pane so eme reads it as ground
// truth instead of guessing from the foreground process. A hook runs INSIDE the agent's
// pane, where $TMUX is already set, so a bare `tmux set-option` reaches whatever server
// the pane lives on (ambient or a pinned -L socket alike) — no socket plumbing needed.
// eme reads the value back via #{@eme_state} in its existing pane snapshot.
//
// The command is written to always exit 0 (a non-zero UserPromptSubmit hook could
// disrupt the agent) and to no-op outside tmux.
const emeHookMarker = "@eme_state"

// emeHookEvents maps each Claude Code hook event to the state it stamps. Ordered for
// deterministic install output. Two caveats are inherent to the events Claude exposes:
//   - Notification also fires for non-permission nudges (e.g. a ~60s idle ping), so a
//     brief `waiting` can show during active work until the next event corrects it.
//   - Stop does NOT fire on a user interrupt (Esc), so an interrupted turn can leave a
//     transient stale `working` that the next prompt (UserPromptSubmit) clears.
var emeHookEvents = []struct{ Event, State string }{
	{"UserPromptSubmit", "working"}, // user submitted a prompt → the agent is now working
	{"Notification", "waiting"},     // agent needs permission / input → waiting for you
	{"Stop", "idle"},                // agent finished its turn → back to idle
}

func emeHookCommand(state string) string {
	return `[ -n "$TMUX" ] && tmux set-option -p -t "$TMUX_PANE" ` + emeHookMarker + ` ` + state + ` || true`
}

// claudeHookCommand / claudeHookGroup model Claude Code's settings.json hooks schema:
// hooks[event] = [ { matcher?, hooks: [ {type, command} ] } ].
type claudeHookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type claudeHookGroup struct {
	Matcher string              `json:"matcher,omitempty"`
	Hooks   []claudeHookCommand `json:"hooks"`
}

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Install agent hooks that report real status into tmux (so eme matches the agent)",
	Long: `eme infers agent status from the pane's foreground process, which cannot tell
working from waiting-for-input from done. Agent hooks close that gap: the agent pushes
its real state into a tmux pane option (@eme_state) that eme reads as ground truth.

  eme hooks install      # wire the hooks into the agent's config (opt-in)
  eme hooks uninstall    # remove them

Currently supports Claude Code (~/.claude/settings.json). Other agents keep the
foreground heuristic. eme only touches its own hooks and preserves everything else.

Assumes one pane per agent window (eme's model): the hook stamps the agent's own pane,
which eme reads as the window's pane. If you split an agent window, the pushed state may
not be the pane eme reads.`,
	RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
}

var hooksAgent string

var hooksInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install eme status hooks into the agent's config",
	RunE:  func(cmd *cobra.Command, args []string) error { return runHooksInstall() },
}

var hooksUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove eme status hooks from the agent's config",
	RunE:  func(cmd *cobra.Command, args []string) error { return runHooksUninstall() },
}

func init() {
	hooksInstallCmd.Flags().StringVar(&hooksAgent, "agent", "claude", "agent whose hooks to manage")
	hooksUninstallCmd.Flags().StringVar(&hooksAgent, "agent", "claude", "agent whose hooks to manage")
	hooksCmd.AddCommand(hooksInstallCmd, hooksUninstallCmd)
}

func claudeSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

func unsupportedAgentErr() error {
	return errors.New(errors.CodeAgentNotFound,
		fmt.Sprintf("Hooks are not supported for agent %q yet.", hooksAgent),
		"Only Claude Code exposes the lifecycle hooks eme needs right now.",
		"Run without --agent (defaults to claude); other agents keep the foreground heuristic.")
}

func runHooksInstall() error {
	if hooksAgent != "claude" {
		return unsupportedAgentErr()
	}
	path, err := claudeSettingsPath()
	if err != nil {
		return err
	}
	existing, existed, err := readIfExists(path)
	if err != nil {
		return err
	}
	merged, added, err := mergeClaudeHooks(existing)
	if err != nil {
		return errors.Wrap(errors.CodeConfigInvalid,
			"Could not update Claude settings.",
			"~/.claude/settings.json could not be read as the expected hooks structure.",
			"Ensure it is valid JSON and that each hooks entry is an array of hook groups.", err)
	}
	if len(added) == 0 {
		fmt.Println("eme status hooks are already installed for claude (nothing to do).")
		return nil
	}
	if existed {
		if err := backupOnce(path+".eme-bak", existing); err != nil {
			return errors.Wrap(errors.CodeCommandFailed,
				"Could not write a settings backup.",
				"Writing the .eme-bak backup failed.",
				"Check write permission on ~/.claude.", err)
		}
	}
	if err := writeFileAtomic(path, merged, 0o644); err != nil {
		return err
	}
	fmt.Printf("Installed eme status hooks for claude into %s:\n", path)
	for _, h := range emeHookEvents {
		fmt.Printf("  %-16s → %s\n", h.Event, h.State)
	}
	if existed {
		fmt.Printf("Backed up your previous settings to %s.eme-bak (other hooks preserved).\n", path)
	}
	fmt.Println("Restart the agent (or start a new one) for the hooks to take effect.")
	return nil
}

func runHooksUninstall() error {
	if hooksAgent != "claude" {
		return unsupportedAgentErr()
	}
	path, err := claudeSettingsPath()
	if err != nil {
		return err
	}
	existing, existed, err := readIfExists(path)
	if err != nil {
		return err
	}
	if !existed {
		fmt.Println("No Claude settings file — nothing to uninstall.")
		return nil
	}
	cleaned, removed, err := removeEmeHooks(existing)
	if err != nil {
		return errors.Wrap(errors.CodeConfigInvalid,
			"Could not update Claude settings.",
			"~/.claude/settings.json could not be read as the expected hooks structure.",
			"Ensure it is valid JSON and that each hooks entry is an array of hook groups.", err)
	}
	if len(removed) == 0 {
		fmt.Println("No eme status hooks found in claude settings (nothing to do).")
		return nil
	}
	if err := writeFileAtomic(path, cleaned, 0o644); err != nil {
		return err
	}
	fmt.Printf("Removed eme status hooks from %s (%s). Other hooks preserved.\n",
		path, strings.Join(removed, ", "))
	return nil
}

// mergeClaudeHooks adds eme's status hooks to a Claude settings.json byte blob without
// disturbing any other key or any non-eme hook. It is idempotent: an event that already
// carries an eme hook is left untouched. Returns the new bytes and the events added.
func mergeClaudeHooks(existing []byte) ([]byte, []string, error) {
	root := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			return nil, nil, err
		}
	}
	if root == nil { // settings.json was literally `null`
		root = map[string]json.RawMessage{}
	}
	hooks := map[string]json.RawMessage{}
	if raw, ok := root["hooks"]; ok {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return nil, nil, err
		}
	}
	if hooks == nil { // "hooks": null — avoid a nil-map assignment panic below
		hooks = map[string]json.RawMessage{}
	}

	var added []string
	for _, h := range emeHookEvents {
		groups, err := decodeRawGroups(hooks[h.Event])
		if err != nil {
			return nil, nil, err
		}
		if rawGroupsHaveEme(groups) {
			continue // idempotent
		}
		// Append eme's OWN group as raw bytes and leave every foreign group as the
		// verbatim json.RawMessage it arrived as — so any extra keys on a user's hook
		// (e.g. a per-command `timeout`) survive untouched. We only ever re-serialize
		// the one group we fully own.
		emeGroup, err := json.Marshal(claudeHookGroup{
			Hooks: []claudeHookCommand{{Type: "command", Command: emeHookCommand(h.State)}},
		})
		if err != nil {
			return nil, nil, err
		}
		groups = append(groups, json.RawMessage(emeGroup))
		raw, err := json.Marshal(groups)
		if err != nil {
			return nil, nil, err
		}
		hooks[h.Event] = raw
		added = append(added, h.Event)
	}
	if len(added) == 0 {
		return existing, nil, nil
	}

	out, err := encodeRootWithHooks(root, hooks)
	return out, added, err
}

// removeEmeHooks strips only eme's status hooks (recognized by their set-option
// @eme_state signature), leaving every other hook byte-for-byte and every other key
// intact. Returns the new bytes and the events touched.
func removeEmeHooks(existing []byte) ([]byte, []string, error) {
	root := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(existing)) == 0 {
		return existing, nil, nil
	}
	if err := json.Unmarshal(existing, &root); err != nil {
		return nil, nil, err
	}
	rawHooks, ok := root["hooks"]
	if !ok {
		return existing, nil, nil
	}
	hooks := map[string]json.RawMessage{}
	if err := json.Unmarshal(rawHooks, &hooks); err != nil {
		return nil, nil, err
	}
	if hooks == nil { // "hooks": null — nothing of ours to remove
		return existing, nil, nil
	}

	var removed []string
	for _, h := range emeHookEvents {
		raw, ok := hooks[h.Event]
		if !ok {
			continue
		}
		groups, err := decodeRawGroups(raw)
		if err != nil {
			return nil, nil, err
		}
		kept := make([]json.RawMessage, 0, len(groups))
		for _, g := range groups {
			if rawGroupHasEme(g) {
				continue // drop only eme's own group
			}
			kept = append(kept, g) // foreign group passes through verbatim
		}
		if len(kept) == len(groups) {
			continue // nothing of ours here
		}
		removed = append(removed, h.Event)
		if len(kept) == 0 {
			delete(hooks, h.Event)
		} else {
			nraw, err := json.Marshal(kept)
			if err != nil {
				return nil, nil, err
			}
			hooks[h.Event] = nraw
		}
	}
	if len(removed) == 0 {
		return existing, nil, nil
	}

	if len(hooks) == 0 {
		delete(root, "hooks")
		out, err := marshalSettings(root)
		return out, removed, err
	}
	out, err := encodeRootWithHooks(root, hooks)
	return out, removed, err
}

// decodeRawGroups splits an event's hook array into per-group raw messages so foreign
// groups can be carried through without being re-serialized from a lossy typed shape.
func decodeRawGroups(raw json.RawMessage) ([]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var groups []json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

// isEmeHookCommand recognizes a command eme installed by its signature — a tmux
// set-option of @eme_state — rather than a bare @eme_state substring, so a foreign
// command that merely mentions @eme_state is never mistaken for (and stripped as) ours.
func isEmeHookCommand(cmd string) bool {
	return strings.Contains(cmd, emeHookMarker) && strings.Contains(cmd, "set-option")
}

func groupsHaveEme(groups []claudeHookGroup) bool {
	for _, g := range groups {
		for _, c := range g.Hooks {
			if isEmeHookCommand(c.Command) {
				return true
			}
		}
	}
	return false
}

// rawGroupHasEme reports whether a raw hook group is one eme installed, reading only
// its commands and never re-serializing it (so a kept foreign group stays byte-exact).
func rawGroupHasEme(raw json.RawMessage) bool {
	var g claudeHookGroup
	if err := json.Unmarshal(raw, &g); err != nil {
		return false
	}
	return groupsHaveEme([]claudeHookGroup{g})
}

func rawGroupsHaveEme(groups []json.RawMessage) bool {
	for _, g := range groups {
		if rawGroupHasEme(g) {
			return true
		}
	}
	return false
}

func encodeRootWithHooks(root, hooks map[string]json.RawMessage) ([]byte, error) {
	hraw, err := json.Marshal(hooks)
	if err != nil {
		return nil, err
	}
	root["hooks"] = hraw
	return marshalSettings(root)
}

func marshalSettings(root map[string]json.RawMessage) ([]byte, error) {
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// backupOnce writes data to path only if it does not already exist, so the FIRST
// install's pristine, pre-eme settings are preserved even across repeated installs (a
// later install must never overwrite the backup with already-modified content).
func backupOnce(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil // a pristine backup is already kept — leave it
		}
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func readIfExists(path string) ([]byte, bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

// writeFileAtomic writes via a temp file + rename so a crash never leaves a half-written
// settings.json. The temp lives in the same dir so the rename stays on one filesystem.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".eme-settings-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
