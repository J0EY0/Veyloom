package hub

import (
	"io/fs"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	veyloom "github.com/J0EY0/veyloom"
	"github.com/J0EY0/veyloom/internal/runtime"
)

// Veyloom's own skills are read once, each folder with a SKILL.md naming
// it and saying what it is for; a folder that is no skill is left out
// (docs/design.md 5.23.6).
func TestReadBuiltinSkills(t *testing.T) {
	fsys := fstest.MapFS{
		"team-practices/SKILL.md":          {Data: []byte("---\nname: team-practices\ndescription: How members\n  work together.\n---\n\n# Team practices\n")},
		"team-practices/reference/five.md": {Data: []byte("# Five questions\n")},
		"team-practices/logo.png":          {Data: []byte{0xff, 0xd8, 0xff}},
		"misnamed/SKILL.md":                {Data: []byte("---\nname: other\ndescription: x\n---\n")},
		"undescribed/SKILL.md":             {Data: []byte("---\nname: undescribed\n---\n")},
		"empty/notes.md":                   {Data: []byte("nothing")},
		"README.md":                        {Data: []byte("# Skills\n")},
	}
	skills, list := readBuiltinSkills(fsys, slog.Default())
	if len(skills) != 1 || skills[0].Name != "team-practices" || !skills[0].Builtin {
		t.Fatalf("skills = %+v", skills)
	}
	files := skills[0].Files
	if _, ok := files["SKILL.md"]; !ok || files["reference/five.md"] != "# Five questions\n" {
		t.Errorf("files = %v", files)
	}
	if _, ok := files["logo.png"]; ok {
		t.Error("a file that is no text travels not")
	}
	if len(list) != 1 || list[0] != (BuiltinSkill{Name: "team-practices", Description: "How members work together."}) {
		t.Errorf("list = %+v", list)
	}
}

// The skills that come with Veyloom read as skills, team-practices among
// them, and say how members hand work back.
func TestVeyloomsOwnSkills(t *testing.T) {
	sub, err := fs.Sub(veyloom.Skills, "skills")
	if err != nil {
		t.Fatal(err)
	}
	skills, list := readBuiltinSkills(sub, slog.Default())
	i := slices.IndexFunc(skills, func(s runtime.Skill) bool { return s.Name == teamPractices })
	if i < 0 || len(list) != len(skills) {
		t.Fatalf("skills = %v, list = %+v", skills, list)
	}
	for _, want := range []string{"## Splitting work", "## Handing work back", "Evidence", "## Checking another's work"} {
		if !strings.Contains(skills[i].Files["SKILL.md"], want) {
			t.Errorf("team-practices does not say %q", want)
		}
	}
}
