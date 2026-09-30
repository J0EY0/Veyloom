package okf

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Frontmatter keys of OKF v0.2 (§4.1, §5), and the Agent Skills fields a
// skill page carries next to them (https://agentskills.io/specification).
const (
	KeyType        = "type"
	KeyTitle       = "title"
	KeyDescription = "description"
	KeyResource    = "resource"
	KeyTags        = "tags"
	KeySources     = "sources"
	KeyUsageWindow = "usage_window"
	KeyGenerated   = "generated"
	KeyVerified    = "verified"
	KeyStatus      = "status"
	KeyStaleAfter  = "stale_after"

	KeyName          = "name"
	KeyLicense       = "license"
	KeyCompatibility = "compatibility"
	KeyMetadata      = "metadata"
	KeyAllowedTools  = "allowed-tools"
)

// Status is a concept's place in its lifecycle (§5.4).
type Status string

const (
	Draft      Status = "draft"
	Stable     Status = "stable"
	Deprecated Status = "deprecated"
)

// Valid reports whether s is one of the three statuses OKF defines.
func (s Status) Valid() bool { return s == Draft || s == Stable || s == Deprecated }

// Stamp is who did something to a concept and when: the generated entry
// and each verified one (§5.2).
type Stamp struct {
	By string
	At time.Time
}

// Source is one entry of sources: something a concept derives from (§5.1).
type Source struct {
	ID           string
	Resource     string
	Title        string
	Author       string
	UsageCount   int64
	LastModified time.Time
}

// Type is the concept's type, the one key OKF requires.
func (d *Document) Type() string { s, _ := d.String(KeyType); return s }

// Title is the display name; empty when not set.
func (d *Document) Title() string { s, _ := d.String(KeyTitle); return s }

// Description is the one-line summary index files and search show.
func (d *Document) Description() string { s, _ := d.String(KeyDescription); return s }

// Resource is the URI of the asset the concept describes, if any.
func (d *Document) Resource() string { s, _ := d.String(KeyResource); return s }

// Name is the Agent Skills name of a skill page.
func (d *Document) Name() string { s, _ := d.String(KeyName); return s }

// Tags lists the tags; anything but a list of strings reads as none.
func (d *Document) Tags() []string {
	v := d.value(KeyTags)
	if v == nil || v.Kind != yaml.SequenceNode {
		return nil
	}
	tags := make([]string, 0, len(v.Content))
	for _, t := range v.Content {
		if t.Kind == yaml.ScalarNode {
			tags = append(tags, t.Value)
		}
	}
	return tags
}

// SetTags replaces the tags, written as a one-line list; none removes the key.
func (d *Document) SetTags(tags []string) {
	if len(tags) == 0 {
		d.Delete(KeyTags)
		return
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, t := range tags {
		seq.Content = append(seq.Content, strNode(t))
	}
	d.set(KeyTags, seq)
}

// Status returns the lifecycle status; a concept without one is stable.
func (d *Document) Status() Status {
	s, ok := d.String(KeyStatus)
	if !ok || s == "" {
		return Stable
	}
	return Status(s)
}

// SetStatus sets the lifecycle status.
func (d *Document) SetStatus(s Status) { d.SetString(KeyStatus, string(s)) }

// Generated returns who last changed the content meaningfully, and when.
func (d *Document) Generated() (Stamp, bool) {
	v := d.value(KeyGenerated)
	if v == nil || v.Kind != yaml.MappingNode {
		return Stamp{}, false
	}
	return stampOf(v), true
}

// SetGenerated records who changed the content and when.
func (d *Document) SetGenerated(s Stamp) { d.set(KeyGenerated, stampNode(s)) }

// Verified lists who confirmed the content and when. A bare mapping reads
// as a list of one, as §5.2 requires of consumers.
func (d *Document) Verified() []Stamp {
	v := d.value(KeyVerified)
	switch {
	case v == nil:
		return nil
	case v.Kind == yaml.MappingNode:
		return []Stamp{stampOf(v)}
	case v.Kind != yaml.SequenceNode:
		return nil
	}
	stamps := make([]Stamp, 0, len(v.Content))
	for _, e := range v.Content {
		if e.Kind == yaml.MappingNode {
			stamps = append(stamps, stampOf(e))
		}
	}
	return stamps
}

// AddVerified records one more confirmation, keeping the ones there.
func (d *Document) AddVerified(s Stamp) {
	entry := stampNode(s)
	v := d.value(KeyVerified)
	switch {
	case v != nil && v.Kind == yaml.SequenceNode:
		v.Content = append(v.Content, entry)
		d.raw = nil
	case v != nil && v.Kind == yaml.MappingNode:
		d.set(KeyVerified, &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{v, entry}})
	default:
		d.set(KeyVerified, &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{entry}})
	}
}

// Sources lists what the concept derives from.
func (d *Document) Sources() []Source {
	v := d.value(KeySources)
	if v == nil || v.Kind != yaml.SequenceNode {
		return nil
	}
	sources := make([]Source, 0, len(v.Content))
	for _, e := range v.Content {
		if e.Kind != yaml.MappingNode {
			continue
		}
		s := Source{
			ID:       scalar(e, "id"),
			Resource: scalar(e, "resource"),
			Title:    scalar(e, "title"),
			Author:   scalar(e, "author"),
		}
		s.UsageCount, _ = strconv.ParseInt(scalar(e, "usage_count"), 10, 64)
		s.LastModified, _ = ParseTime(scalar(e, "last_modified"))
		sources = append(sources, s)
	}
	return sources
}

// AddSource appends one entry to sources, keeping the ones there.
func (d *Document) AddSource(s Source) {
	entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	pair := func(k string, v *yaml.Node) { entry.Content = append(entry.Content, strNode(k), v) }
	if s.ID != "" {
		pair("id", strNode(s.ID))
	}
	pair("resource", strNode(s.Resource))
	if s.Title != "" {
		pair("title", strNode(s.Title))
	}
	if s.Author != "" {
		pair("author", strNode(s.Author))
	}
	if s.UsageCount != 0 {
		pair("usage_count", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(s.UsageCount, 10)})
	}
	if !s.LastModified.IsZero() {
		pair("last_modified", timeNode(s.LastModified))
	}
	if v := d.value(KeySources); v != nil && v.Kind == yaml.SequenceNode {
		v.Content = append(v.Content, entry)
		d.raw = nil
		return
	}
	d.set(KeySources, &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{entry}})
}

// ReplaceSourceResource points every source whose resource is from at to
// instead, and reports how many it changed.
func (d *Document) ReplaceSourceResource(from, to string) int {
	v := d.value(KeySources)
	if v == nil || v.Kind != yaml.SequenceNode {
		return 0
	}
	n := 0
	for _, e := range v.Content {
		if r := valueOf(e, "resource"); r != nil && r.Kind == yaml.ScalarNode && r.Value == from {
			r.Value = to
			n++
		}
	}
	if n > 0 {
		d.raw = nil
	}
	return n
}

// StaleAfter returns the instant the content goes stale, if one is set.
func (d *Document) StaleAfter() (time.Time, bool) {
	s, ok := d.String(KeyStaleAfter)
	if !ok {
		return time.Time{}, false
	}
	t, err := ParseTime(s)
	return t, err == nil
}

// SetStaleAfter sets the instant the content goes stale.
func (d *Document) SetStaleAfter(t time.Time) { d.set(KeyStaleAfter, timeNode(t)) }

// Metadata returns a skill page's Agent Skills metadata, a map of strings.
func (d *Document) Metadata() map[string]string {
	v := d.value(KeyMetadata)
	if v == nil || v.Kind != yaml.MappingNode {
		return nil
	}
	m := make(map[string]string, len(v.Content)/2)
	for i := 0; i+1 < len(v.Content); i += 2 {
		if v.Content[i+1].Kind == yaml.ScalarNode {
			m[v.Content[i].Value] = v.Content[i+1].Value
		}
	}
	return m
}

// DeleteMetadata removes one entry of a skill page's metadata, and the
// metadata itself once it is empty.
func (d *Document) DeleteMetadata(key string) {
	v := d.value(KeyMetadata)
	if v == nil || v.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(v.Content); i += 2 {
		if v.Content[i].Value == key {
			d.raw = nil
			v.Content = slices.Delete(v.Content, i, i+2)
			break
		}
	}
	if len(v.Content) == 0 {
		d.Delete(KeyMetadata)
	}
}

// AgentSkill is a skill page as Agent Skills has it: the same body, and
// the frontmatter without OKF's keys, the library's own record of the page
// (description aside, which both have). What a runtime loads: Agent
// Skills' fields and whatever the runtimes add to them, as they were
// written.
func (d *Document) AgentSkill() *Document {
	c := d.Clone()
	for _, k := range c.Keys() {
		if k != KeyDescription && slices.Contains(okfKeys, k) {
			c.Delete(k)
		}
	}
	return c
}

// SkillSettings are the fields of a skill's SKILL.md that tell runtimes
// how to use it, each as YAML text: all but OKF's, its name and its
// metadata. allowed-tools, disable-model-invocation, hooks and the like:
// a person sets them (docs/design.md 5.15), an agent improving the skill
// leaves them as they are.
func (d *Document) SkillSettings() map[string]string {
	out := map[string]string{}
	for _, k := range d.Keys() {
		if slices.Contains(okfKeys, k) || k == KeyName || k == KeyMetadata {
			continue
		}
		text, err := yaml.Marshal(d.value(k))
		if err != nil {
			text = []byte(d.value(k).Value)
		}
		out[k] = string(text)
	}
	return out
}

// SetMetadata sets one entry of a skill page's metadata.
func (d *Document) SetMetadata(key, value string) {
	v := d.value(KeyMetadata)
	if v == nil || v.Kind != yaml.MappingNode {
		v = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		d.set(KeyMetadata, v)
	}
	d.raw = nil
	for i := 0; i+1 < len(v.Content); i += 2 {
		if v.Content[i].Value == key {
			v.Content[i+1] = strNode(value)
			return
		}
	}
	v.Content = append(v.Content, strNode(key), strNode(value))
}

// ParseTime reads an OKF timestamp: ISO 8601 with an explicit UTC offset.
func ParseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
}

// FormatTime writes a timestamp the way Veyloom stores them: UTC, seconds.
func FormatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func stampOf(m *yaml.Node) Stamp {
	at, _ := ParseTime(scalar(m, "at"))
	return Stamp{By: scalar(m, "by"), At: at}
}

// stampNode writes a stamp as a block mapping: in a one-line mapping the
// YAML encoder would quote both values for their colons.
func stampNode(s Stamp) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		strNode("by"), strNode(s.By), strNode("at"), timeNode(s.At),
	}}
}

func strNode(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }

// timeNode is a timestamp scalar, written plain rather than quoted.
func timeNode(t time.Time) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: FormatTime(t)}
}

func valueOf(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func scalar(m *yaml.Node, key string) string {
	if v := valueOf(m, key); v != nil && v.Kind == yaml.ScalarNode {
		return v.Value
	}
	return ""
}
