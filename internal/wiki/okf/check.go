package okf

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// Profile picks which rules a check applies.
type Profile int

const (
	// Conformance is what OKF v0.2 §11 requires of any bundle. Veyloom
	// reads every bundle that passes it, including ones it did not write.
	Conformance Profile = iota
	// Strict adds what Veyloom holds its own writes to (design.md 5.13).
	Strict
)

// Problem is one thing wrong with a file.
type Problem struct {
	Path    string // in the bundle, like "/decisions/x.md"
	Rule    string
	Message string
}

func (p Problem) String() string { return fmt.Sprintf("%s: %s (%s)", p.Path, p.Message, p.Rule) }

// Reserved file names (§3.1): they are never concepts.
const (
	IndexFile = "index.md"
	LogFile   = "log.md"
)

// SkillFile is the file an Agent Skills skill lives in.
const SkillFile = "SKILL.md"

// CheckFile checks one markdown file of a bundle. root says whether it
// sits at the bundle root, where index.md may declare the OKF version.
func CheckFile(p string, data []byte, root bool, profile Profile) []Problem {
	switch path.Base(p) {
	case IndexFile:
		return checkIndex(p, data, root)
	case LogFile:
		return checkLog(p, data, profile)
	}
	d, err := Parse(data)
	if err != nil {
		return []Problem{{p, "frontmatter", err.Error()}}
	}
	return CheckConcept(p, d, profile)
}

// CheckConcept checks a concept document that is to live at p.
func CheckConcept(p string, d *Document, profile Profile) []Problem {
	c := checker{path: p}
	if base := path.Base(p); base == IndexFile || base == LogFile {
		c.add("reserved", "%s is reserved for the bundle's own listing and history, not for a concept", base)
	}
	if t, ok := d.String(KeyType); !ok || strings.TrimSpace(t) == "" {
		c.add("type", "the frontmatter needs a type")
	}
	if profile == Strict {
		c.strict(d)
	}
	return c.problems
}

type checker struct {
	path     string
	problems []Problem
}

func (c *checker) add(rule, format string, args ...any) {
	c.problems = append(c.problems, Problem{c.path, rule, fmt.Sprintf(format, args...)})
}

// okfKeys are the frontmatter keys OKF v0.2 defines; skillKeys the ones a
// skill page adds from Agent Skills.
var (
	okfKeys = []string{
		KeyType, KeyTitle, KeyDescription, KeyResource, KeyTags, KeySources, KeyUsageWindow,
		KeyGenerated, KeyVerified, KeyStatus, KeyStaleAfter,
		"runtime", "parameters", "computation", "executor", "attester",
	}
	skillKeys = []string{KeyName, KeyLicense, KeyCompatibility, KeyMetadata, KeyAllowedTools}
)

// KeepToSpec leaves a document read from elsewhere the fields OKF v0.2
// defines, and on a skill's SKILL.md the ones Agent Skills adds. A field of
// neither that holds one value moves into a skill's metadata, where Agent
// Skills lets a client keep its own; anything else is dropped. It returns
// the keys dropped.
func (d *Document) KeepToSpec(skill bool) []string {
	var dropped []string
	for _, k := range d.Keys() {
		if slices.Contains(okfKeys, k) || (skill && slices.Contains(skillKeys, k)) {
			continue
		}
		if v, ok := d.String(k); ok && skill && v != "" {
			d.SetMetadata(k, v)
		} else {
			dropped = append(dropped, k)
		}
		d.Delete(k)
	}
	return dropped
}

func (c *checker) strict(d *Document) {
	isSkill := d.Type() == "Skill" && path.Base(c.path) == SkillFile
	seen := map[string]bool{}
	for _, k := range d.Keys() {
		if seen[k] {
			c.add("key", "%s appears twice", k)
		}
		seen[k] = true
		if !slices.Contains(okfKeys, k) && !(isSkill && slices.Contains(skillKeys, k)) {
			c.add("key", "%s is not a field of OKF v0.2; Veyloom writes only the fields the spec defines", k)
		}
	}
	if s := d.Status(); !s.Valid() {
		c.add("status", "status %q is not draft, stable or deprecated", s)
	}
	if v := d.value(KeyTags); v != nil && !isStringList(v) {
		c.add("tags", "tags should be a list of strings")
	}
	if v := d.value(KeyGenerated); v != nil {
		c.stamp(KeyGenerated, v)
	}
	if v := d.value(KeyVerified); v != nil {
		entries := []*yaml.Node{v}
		if v.Kind == yaml.SequenceNode {
			entries = v.Content
		}
		for _, e := range entries {
			c.stamp(KeyVerified, e)
		}
	}
	if s, ok := d.String(KeyStaleAfter); d.Has(KeyStaleAfter) && (!ok || !isTime(s)) {
		c.add("timestamp", "stale_after should be an ISO 8601 time with a UTC offset")
	}
	c.sources(d)
	c.links(d)
	if isSkill {
		c.skill(d)
	}
}

func (c *checker) stamp(key string, v *yaml.Node) {
	if v.Kind != yaml.MappingNode {
		c.add(key, "each %s entry should be a mapping with by and at", key)
		return
	}
	if by := scalar(v, "by"); !ValidActor(by) {
		c.add("actor", "%s.by %q should be an actor: human:<id>, process:<id> or <producer>/<version>", key, by)
	}
	if !isTime(scalar(v, "at")) {
		c.add("timestamp", "%s.at should be an ISO 8601 time with a UTC offset", key)
	}
}

func (c *checker) sources(d *Document) {
	v := d.value(KeySources)
	if v == nil {
		return
	}
	if v.Kind != yaml.SequenceNode {
		c.add("sources", "sources should be a list")
		return
	}
	ids := map[string]bool{}
	for _, e := range v.Content {
		r := scalar(e, "resource")
		if e.Kind != yaml.MappingNode || r == "" {
			c.add("sources", "every source needs a resource")
			continue
		}
		if id := scalar(e, "id"); id != "" {
			if ids[id] {
				c.add("sources", "source id %q is used twice", id)
			}
			ids[id] = true
		}
		if relativePath(r) {
			c.add("path", "source %q should be written from the bundle root, starting with /", r)
		}
		if lm := scalar(e, "last_modified"); lm != "" && !isTime(lm) {
			c.add("timestamp", "last_modified of source %q should be an ISO 8601 time with a UTC offset", r)
		}
	}
	cited, _ := Footnotes(d.Body())
	for _, label := range cited {
		if !ids[label] {
			c.add("footnote", "footnote [^%s] has no source with id %q: give the page a source with that id, or drop the footnote", label, label)
		}
	}
}

func (c *checker) links(d *Document) {
	skill := d.Type() == "Skill" && path.Base(c.path) == SkillFile
	for _, l := range Links(d.Body()) {
		if IsExternal(l.Target) || strings.HasPrefix(l.Target, "/") || strings.HasPrefix(l.Target, "#") {
			continue
		}
		// A skill links to its own files as Agent Skills does, from its
		// directory: that is where a runtime finds them once it is
		// loaded, far from the bundle's root.
		if skill && inDir(c.path, l.Target) {
			continue
		}
		c.add("link", "link %q should start with / (a path from the bundle root)", l.Target)
	}
}

// inDir reports whether a relative link from the page at p stays within
// the page's directory.
func inDir(p, target string) bool {
	to, ok := Resolve(p, target)
	dir := path.Dir(p)
	return ok && strings.HasPrefix(to, dir+"/")
}

// skillName is the Agent Skills rule for names: lowercase letters, digits
// and single hyphens, at most 64 characters.
var skillName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func (c *checker) skill(d *Document) {
	name := d.Name()
	switch {
	case name == "":
		c.add("skill", "a skill needs a name")
	case len(name) > 64 || !skillName.MatchString(name):
		c.add("skill", "skill name %q should be lowercase letters, digits and single hyphens, at most 64 characters", name)
	case name != path.Base(path.Dir(c.path)):
		c.add("skill", "skill name %q should match its directory %q", name, path.Base(path.Dir(c.path)))
	}
	if desc := d.Description(); desc == "" || utf8.RuneCountInString(desc) > 1024 {
		c.add("skill", "a skill needs a description of at most 1024 characters")
	}
	if s, _ := d.String(KeyCompatibility); utf8.RuneCountInString(s) > 500 {
		c.add("skill", "compatibility should be at most 500 characters")
	}
	if v := d.value(KeyMetadata); v != nil && !isStringMap(v) {
		c.add("skill", "metadata should map strings to strings")
	}
}

// checkIndex checks an index.md (§8): no frontmatter, except that the
// bundle root's may declare okf_version.
func checkIndex(p string, data []byte, root bool) []Problem {
	c := checker{path: p}
	front, _, err := split(data)
	if err != nil {
		return nil
	}
	m, err := parseMapping(front)
	switch {
	case err != nil:
		c.add("index", "the frontmatter does not parse: %v", err)
	case !root:
		c.add("index", "only the bundle root's index.md may have frontmatter")
	default:
		for i := 0; i+1 < len(m.Content); i += 2 {
			if k := m.Content[i].Value; k != "okf_version" {
				c.add("index", "the root index.md may only declare okf_version, not %s", k)
			}
		}
	}
	return c.problems
}

func isStringList(v *yaml.Node) bool {
	if v.Kind != yaml.SequenceNode {
		return false
	}
	for _, e := range v.Content {
		if e.Kind != yaml.ScalarNode {
			return false
		}
	}
	return true
}

func isStringMap(v *yaml.Node) bool {
	if v.Kind != yaml.MappingNode {
		return false
	}
	for i := 1; i < len(v.Content); i += 2 {
		if v.Content[i].Kind != yaml.ScalarNode {
			return false
		}
	}
	return true
}

func isTime(s string) bool { _, err := ParseTime(s); return err == nil }

// relativePath reports whether a source resource looks like a path in the
// bundle written without the leading slash. OKF §6.2 and its own examples
// read such paths differently (issue #29), so Veyloom never writes them.
func relativePath(r string) bool {
	if IsExternal(r) || strings.HasPrefix(r, "/") || strings.ContainsAny(r, " \t") {
		return false
	}
	return strings.HasSuffix(r, ".md") || strings.Contains(r, "/")
}
