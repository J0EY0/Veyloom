package hub

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// Skills as a person keeps them for their runtime, brought into the
// library and given to the real CLIs (docs/design.md 5.10, 5.11), on the
// same terms as the other smoke tests: VEYLOOM_CLAUDE_REAL_SMOKE=1 or
// VEYLOOM_CODEX_SMOKE=1, a test database, the CLI installed and signed in.

// TestClaudeRealSmoke_SkillSettings: what a skill says to the runtime
// reaches it as it was written. disable-model-invocation keeps Claude Code
// from a skill, so the model does not have the word only that skill holds;
// a skill with other fields runtimes add is used as any other.
func TestClaudeRealSmoke_SkillSettings(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Claude skills", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, t.TempDir())
	turn := r.settingsRound(1, "", "gate-word", "gate", "TULIP-31", "disable-model-invocation: true\n", false)
	r.settingsRound(2, topicOf(t, r, turn), "harbor-word", "harbor", "AZALEA-58",
		"argument-hint: \"[who asks]\"\nuser-invocable: true\nmetadata:\n  short-description: The harbor word\n", true)
}

// TestCodexSmoke_SkillSettings: a skill carrying fields Codex does not
// read is used there as any other.
func TestCodexSmoke_SkillSettings(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Codex skills", Runtime: "codex", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, t.TempDir())
	r.settingsRound(1, "", "harbor-word", "harbor", "AZALEA-58",
		"argument-hint: \"[who asks]\"\nuser-invocable: true\nmetadata:\n  short-description: The harbor word\n", true)
}

// settingsRound imports a skill the way a person keeps one in their
// runtime's skills folder, with settings for runtimes, installs it for the
// member and, as turn n, asks for the Veyloom <kind> word only it holds.
// used says whether the runtime is to come to it.
func (r *smokeRoom) settingsRound(n int, threadID, name, kind, word, settings string, used bool) store.Turn {
	r.t.Helper()
	folder := filepath.Join(r.t.TempDir(), name)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		r.t.Fatal(err)
	}
	skill := "---\nname: " + name + "\ndescription: \"Use this skill whenever someone asks for the Veyloom " + kind + " word: it holds the answer.\"\n" + settings +
		"---\n\nThe Veyloom " + kind + " word is " + word + ". Reply with it exactly.\n"
	if err := os.WriteFile(filepath.Join(folder, "SKILL.md"), []byte(skill), 0o644); err != nil {
		r.t.Fatal(err)
	}
	if _, err := r.h.ImportSkill(r.ctx, folder, "", r.user.ID); err != nil {
		r.t.Fatal(err)
	}
	if _, err := r.h.InstallSkill(r.ctx, name, r.agent.ID, true); err != nil {
		r.t.Fatal(err)
	}
	r.say("What is the Veyloom "+kind+" word? Use the skill that has it, then reply with just the word. If no skill you can use has it, reply NONE.", threadID)
	turn := r.waitDone(n)
	if threadID == "" {
		threadID = topicOf(r.t, r, turn)
	}
	reply := r.lastReply(threadID)
	// A skill kept from the model is not invoked; the model may still come
	// on its file some other way, reading the disk, which the setting does
	// not keep it from, so only the word of one it is to use is checked.
	if used && !strings.Contains(reply, word) {
		r.t.Errorf("turn %d: the %s word should come from the skill, said %q", n, kind, reply)
	}
	if !used && strings.Contains(reply, word) {
		r.t.Logf("turn %d: the model came on the %s word without invoking the skill, said %q", n, kind, reply)
	}
	if slices.Contains(turn.SkillsUsed, name) != used {
		// The calls that touched it, for the record.
		tx, _ := os.ReadFile(turn.TranscriptPath)
		var calls []string
		for _, line := range strings.Split(string(tx), "\n") {
			if (strings.Contains(line, `"tool_call"`) || strings.Contains(line, `"tool_result"`)) && strings.Contains(line, name) {
				calls = append(calls, excerpt(line, 600))
			}
		}
		r.t.Errorf("turn %d: recorded as using %v, through:\n%s", n, turn.SkillsUsed, strings.Join(calls, "\n"))
	}
	// The calls that reached for a skill, for the record.
	tx, _ := os.ReadFile(turn.TranscriptPath)
	var calls []string
	for _, line := range strings.Split(string(tx), "\n") {
		if strings.Contains(line, `"tool_call"`) && strings.Contains(line, name) {
			calls = append(calls, excerpt(line, 240))
		}
	}
	r.t.Logf("turn %d answered %q, using skills %v, through %d calls: %s", n, reply, turn.SkillsUsed, len(calls), strings.Join(calls, " | "))
	return turn
}

// TestClaudeRealSmoke_SkillReferences: a skill's references reach Claude
// Code as the plain markdown they were, linking from where they are, so it
// follows the skill to its guide and on to the list the word is in.
func TestClaudeRealSmoke_SkillReferences(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Claude skills", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, t.TempDir())
	r.referencesRound(1, "")
}

// TestCodexSmoke_SkillReferences is TestClaudeRealSmoke_SkillReferences
// on Codex.
func TestCodexSmoke_SkillReferences(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Codex skills", Runtime: "codex", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, t.TempDir())
	r.referencesRound(1, "")
}

// TestClaudeRealSmoke_SkillExport: a skill taken out of the library is the
// folder it would be anywhere else. Unzipped into a project's
// .claude/skills, Claude Code run there, with no Veyloom about, loads it
// and finds the word through its references.
func TestClaudeRealSmoke_SkillExport(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionReadOnly, RoleCard: "x"}, t.TempDir())
	elsewhere := r.exportRelay(".claude/skills")
	cmd := exec.Command("claude", "-p", relayQuestion, "--model", "haiku", "--output-format", "stream-json", "--verbose", "--max-turns", "10")
	cmd.Dir = elsewhere
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("claude: %v\n%s", err, out)
	}
	var skills []string
	var answer string
	scan := bufio.NewScanner(bytes.NewReader(out))
	scan.Buffer(make([]byte, 1<<20), 16<<20)
	for scan.Scan() {
		var line struct {
			Type    string   `json:"type"`
			Subtype string   `json:"subtype"`
			Skills  []string `json:"skills"`
			Result  string   `json:"result"`
		}
		if json.Unmarshal(scan.Bytes(), &line) != nil {
			continue
		}
		if line.Type == "system" && line.Subtype == "init" {
			skills = line.Skills
		}
		if line.Type == "result" {
			answer = line.Result
		}
	}
	if !slices.Contains(skills, "relay-word") {
		t.Errorf("claude should load the skill from .claude/skills, loaded %v", skills)
	}
	if !strings.Contains(answer, "FERN-77") {
		t.Errorf("claude should find the word through the skill's references, said %q", answer)
	}
	t.Logf("claude loaded %d skills, relay-word among them: %v; answered %q", len(skills), slices.Contains(skills, "relay-word"), answer)
}

// TestCodexSmoke_SkillExport is TestClaudeRealSmoke_SkillExport on Codex,
// the skill unzipped into .agents/skills, where Codex looks in a project.
func TestCodexSmoke_SkillExport(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionReadOnly, RoleCard: "x"}, t.TempDir())
	elsewhere := r.exportRelay(".agents/skills")
	last := filepath.Join(t.TempDir(), "last.txt")
	args := []string{"exec", "--skip-git-repo-check", "--ephemeral", "--disable", "memories", "-s", "read-only", "--json", "-C", elsewhere, "-o", last}
	if model := os.Getenv("VEYLOOM_CODEX_SMOKE_MODEL"); model != "" {
		args = append(args, "-m", model)
	}
	out, err := exec.Command("codex", append(args, relayQuestion)...).CombinedOutput()
	if err != nil {
		t.Fatalf("codex: %v\n%s", err, excerpt(string(out), 4000))
	}
	answer, _ := os.ReadFile(last)
	if !strings.Contains(string(answer), "FERN-77") {
		t.Errorf("codex should find the word through the skill's references, said %q:\n%s", answer, excerpt(string(out), 4000))
	}
	// How it came to it: the commands it ran on the skill's files.
	var reads []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "command_execution") && strings.Contains(line, "relay-word") {
			reads = append(reads, excerpt(line, 300))
		}
	}
	t.Logf("codex answered %q through %d commands on the skill: %s", strings.TrimSpace(string(answer)), len(reads), strings.Join(reads, " | "))
}

// relayQuestion asks for the word the relay skill keeps two links away.
const relayQuestion = "What is the Veyloom relay word? Use the skill that has it and follow its references, then reply with just the word."

// relayFolder is a skill as a person keeps one in their runtime's skills
// folder, holding the word two links away: SKILL.md points to its guide,
// the guide to the list next to it, each linking from where it is.
func relayFolder(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "relay-word")
	for rel, text := range map[string]string{
		"SKILL.md": "---\nname: relay-word\ndescription: \"Use this skill whenever someone asks for the Veyloom relay word: it tells where the word is kept.\"\n---\n\n" +
			"The Veyloom relay word is kept where [the guide](references/guide.md) says. Read the guide, follow its link, and reply with the word exactly.\n",
		"references/guide.md": "# Guide\n\nThe relay word is written in [the list](list.md), next to this guide.\n",
		"references/list.md":  "# List\n\nThe Veyloom relay word is FERN-77.\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// referencesRound imports the relay skill, installs it for the member and,
// as turn n, asks for the word only its list holds.
func (r *smokeRoom) referencesRound(n int, threadID string) store.Turn {
	r.t.Helper()
	if _, err := r.h.ImportSkill(r.ctx, relayFolder(r.t), "", r.user.ID); err != nil {
		r.t.Fatal(err)
	}
	if _, err := r.h.InstallSkill(r.ctx, "relay-word", r.agent.ID, true); err != nil {
		r.t.Fatal(err)
	}
	r.say(relayQuestion, threadID)
	turn := r.waitDone(n)
	if threadID == "" {
		threadID = topicOf(r.t, r, turn)
	}
	reply := r.lastReply(threadID)
	tx, _ := os.ReadFile(turn.TranscriptPath)
	var calls []string
	for _, line := range strings.Split(string(tx), "\n") {
		if strings.Contains(line, `"tool_call"`) && strings.Contains(line, "relay-word") {
			calls = append(calls, excerpt(line, 300))
		}
	}
	if !strings.Contains(reply, "FERN-77") {
		r.t.Errorf("turn %d should have found the word through the skill's references, said %q, through:\n%s", n, reply, strings.Join(calls, "\n"))
	}
	if !slices.Contains(turn.SkillsUsed, "relay-word") {
		r.t.Errorf("turn %d should be recorded as using the skill: %v", n, turn.SkillsUsed)
	}
	r.t.Logf("turn %d answered %q through %d calls on the skill: %s", n, reply, len(calls), strings.Join(calls, " | "))
	return turn
}

// exportRelay imports the relay skill and unzips the library's export of
// it into skills, a skills folder of a new project elsewhere, which it
// returns.
func (r *smokeRoom) exportRelay(skills string) string {
	r.t.Helper()
	if _, err := r.h.ImportSkill(r.ctx, relayFolder(r.t), "", r.user.ID); err != nil {
		r.t.Fatal(err)
	}
	zipped, err := r.h.ExportSkill(r.ctx, "relay-word")
	if err != nil {
		r.t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(zipped), int64(len(zipped)))
	if err != nil {
		r.t.Fatal(err)
	}
	elsewhere := r.t.TempDir()
	for _, f := range archive.File {
		p := filepath.Join(elsewhere, filepath.FromSlash(skills), filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			r.t.Fatal(err)
		}
		rc, err := f.Open()
		if err != nil {
			r.t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		if err := os.WriteFile(p, data, f.Mode().Perm()); err != nil {
			r.t.Fatal(err)
		}
	}
	return elsewhere
}

// TestClaudeRealSmoke_SkillBlobs: a skill as people share them, past the
// limits the library once had, reaches Claude Code whole, its files that
// are not text byte for byte: the word is the start of a checksum of one,
// which Claude Code works out with a command.
func TestClaudeRealSmoke_SkillBlobs(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	// Full auto, in a folder of its own: it runs shasum without asking.
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Claude skills", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionFullAuto,
		RoleCard: "Answer in as few words as you can.",
	}, t.TempDir())
	r.blobRound(1, "")
}

// TestCodexSmoke_SkillBlobs is TestClaudeRealSmoke_SkillBlobs on Codex,
// whose read-only sandbox runs a command that only reads.
func TestCodexSmoke_SkillBlobs(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Codex skills", Runtime: "codex", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, t.TempDir())
	r.blobRound(1, "")
}

// blobRound imports a skill of 93 files, one of them 300 KB of text and
// one 70 KB of random bytes, installs it for the member and, as turn n,
// asks for the word the random bytes make: the first 12 hex digits of
// their SHA-256.
func (r *smokeRoom) blobRound(n int, threadID string) store.Turn {
	r.t.Helper()
	blob := make([]byte, 70<<10)
	if _, err := rand.Read(blob); err != nil {
		r.t.Fatal(err)
	}
	sum := sha256.Sum256(blob)
	word := hex.EncodeToString(sum[:])[:12]
	dir := filepath.Join(r.t.TempDir(), "checksum-word")
	files := map[string][]byte{
		"SKILL.md": []byte("---\nname: checksum-word\ndescription: \"Use this skill whenever someone asks for the Veyloom checksum word: it says how to work it out.\"\n---\n\n" +
			"The Veyloom checksum word is the first 12 hex digits of the SHA-256 of the file assets/blob.bin in this skill's folder. " +
			"Work it out with a command such as `shasum -a 256 <that file>` and reply with just those 12 digits.\n"),
		"assets/blob.bin":   blob,
		"notes/history.txt": bytes.Repeat([]byte("A line of the skill's long history, kept for the record.\n"), 300<<10/56),
	}
	for i := range 90 {
		files[fmt.Sprintf("notes/note-%02d.txt", i)] = []byte("A note.\n")
	}
	for rel, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
	if _, err := r.h.ImportSkill(r.ctx, dir, "", r.user.ID); err != nil {
		r.t.Fatal(err)
	}
	if _, err := r.h.InstallSkill(r.ctx, "checksum-word", r.agent.ID, true); err != nil {
		r.t.Fatal(err)
	}
	r.say("What is the Veyloom checksum word? Use the skill that has it, then reply with just the word.", threadID)
	turn := r.waitDone(n)
	if threadID == "" {
		threadID = topicOf(r.t, r, turn)
	}
	reply := r.lastReply(threadID)
	tx, _ := os.ReadFile(turn.TranscriptPath)
	var calls []string
	for _, line := range strings.Split(string(tx), "\n") {
		if strings.Contains(line, `"tool_call"`) && strings.Contains(line, "checksum-word") {
			calls = append(calls, excerpt(line, 300))
		}
	}
	if !strings.Contains(reply, word) {
		r.t.Errorf("turn %d should have worked out %s from the skill's file, said %q, through:\n%s", n, word, reply, strings.Join(calls, "\n"))
	}
	if !slices.Contains(turn.SkillsUsed, "checksum-word") {
		r.t.Errorf("turn %d should be recorded as using the skill: %v", n, turn.SkillsUsed)
	}
	r.t.Logf("turn %d answered %q (want %s) through %d calls on the skill: %s", n, reply, word, len(calls), strings.Join(calls, " | "))
	return turn
}

// TestClaudeRealSmoke_SkillOverPersons: a person keeps a skill of the
// name of one installed for the agent, in the project's .claude/skills;
// the turn uses the installed one, the person's left out for it.
func TestClaudeRealSmoke_SkillOverPersons(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	dir := t.TempDir()
	personsDupSkill(t, filepath.Join(dir, ".claude", "skills"))
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Claude skills", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, dir)
	turn := r.settingsRound(1, "", "dup-word", "dup", "LIBRARY-22", "", true)
	if reply := r.lastReply(topicOf(t, r, turn)); strings.Contains(reply, "PERSONAL-11") {
		t.Errorf("the person's own skill should be out of the turn, said %q", reply)
	}
}

// TestCodexSmoke_SkillOverPersons is TestClaudeRealSmoke_SkillOverPersons
// on Codex, the person's skill in the project's .agents/skills.
func TestCodexSmoke_SkillOverPersons(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	dir := t.TempDir()
	personsDupSkill(t, filepath.Join(dir, ".agents", "skills"))
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Codex skills", Runtime: "codex", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, dir)
	turn := r.settingsRound(1, "", "dup-word", "dup", "LIBRARY-22", "", true)
	if reply := r.lastReply(topicOf(t, r, turn)); strings.Contains(reply, "PERSONAL-11") {
		t.Errorf("the person's own skill should be out of the turn, said %q", reply)
	}
}

// personsDupSkill puts a person's own dup-word skill in skills, holding a
// word of its own: PERSONAL-11.
func personsDupSkill(t *testing.T, skills string) {
	t.Helper()
	dir := filepath.Join(skills, "dup-word")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	skill := "---\nname: dup-word\ndescription: \"Use this skill whenever someone asks for the Veyloom dup word: it holds the answer.\"\n---\n\nThe Veyloom dup word is PERSONAL-11. Reply with it exactly.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCodexSmoke_SkillBesideBuiltin: a skill of the library with the name
// of one Codex comes with, skill-creator, which OpenAI keeps up to date:
// Codex's own stays on, and the library's comes as veyloom-skill-creator
// beside it, used as any other. It needs a Codex with that skill.
func TestCodexSmoke_SkillBesideBuiltin(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, _ := os.UserHomeDir()
		codexHome = filepath.Join(home, ".codex")
	}
	if _, err := os.Stat(filepath.Join(codexHome, "skills", ".system", "skill-creator", "SKILL.md")); err != nil {
		t.Skipf("this codex comes without skill-creator: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Codex skills", Runtime: "codex", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, t.TempDir())
	turn := r.settingsRound(1, "", "skill-creator", "creator", "BIRCH-33", "", true)
	tx, _ := os.ReadFile(turn.TranscriptPath)
	if !strings.Contains(string(tx), "skill-creator goes by veyloom-skill-creator") || strings.Contains(string(tx), "enabled=false") {
		t.Errorf("the library's should go by another name, Codex's own staying on:\n%s", excerpt(string(tx), 3000))
	}
}
