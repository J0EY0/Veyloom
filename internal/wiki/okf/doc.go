// Package okf reads and writes the Open Knowledge Format (OKF v0.2): a
// knowledge bundle is a directory of markdown files with YAML frontmatter,
// one concept per file, plus the reserved index.md and log.md. It knows the
// format only; where bundles live and who writes them is package wiki's
// business (design.md 5.3, 5.9 and 5.13).
//
// The spec lives at
// https://github.com/GoogleCloudPlatform/open-knowledge-format/blob/main/SPEC.md.
package okf

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Version is the OKF version Veyloom writes; a bundle's root index.md
// declares it.
const Version = "0.2"

var (
	// ErrNoFrontmatter reports a markdown file that does not open with a
	// frontmatter block, which OKF requires of every concept.
	ErrNoFrontmatter = errors.New("okf: no frontmatter")
	// ErrBadFrontmatter reports a frontmatter block that is not closed or
	// is not a YAML mapping.
	ErrBadFrontmatter = errors.New("okf: bad frontmatter")
)

// Document is one concept file. The frontmatter is kept as a YAML node
// tree, so keys this package does not know, their order and their comments
// survive a round trip, and a document nothing was changed in writes back
// exactly the bytes it was read from.
type Document struct {
	front *yaml.Node // always a mapping
	body  string
	raw   []byte // the bytes parsed, until something changes
}

// New starts a document of the given type with an empty body.
func New(typ string) *Document {
	d := &Document{front: &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}}
	d.SetString(KeyType, typ)
	return d
}

// Parse reads a concept file.
func Parse(data []byte) (*Document, error) {
	front, body, err := split(data)
	if err != nil {
		return nil, err
	}
	node, err := parseMapping(front)
	if err != nil {
		return nil, err
	}
	return &Document{front: node, body: string(body), raw: bytes.Clone(data)}, nil
}

// Clone returns an independent copy.
func (d *Document) Clone() *Document {
	c := &Document{front: cloneNode(d.front), body: d.body}
	if d.raw != nil {
		c.raw = bytes.Clone(d.raw)
	}
	return c
}

// Bytes renders the file. A document read by Parse and not changed since
// renders as the bytes it was read from.
func (d *Document) Bytes() ([]byte, error) {
	if d.raw != nil {
		return bytes.Clone(d.raw), nil
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	if len(d.front.Content) > 0 {
		enc := yaml.NewEncoder(&b)
		enc.SetIndent(2)
		if err := enc.Encode(d.front); err != nil {
			return nil, fmt.Errorf("okf: encode frontmatter: %w", err)
		}
		if err := enc.Close(); err != nil {
			return nil, fmt.Errorf("okf: encode frontmatter: %w", err)
		}
	}
	b.WriteString("---\n")
	b.WriteString(d.body)
	return b.Bytes(), nil
}

// Body returns the markdown after the frontmatter, without the blank line
// that usually separates the two.
func (d *Document) Body() string {
	if s, ok := strings.CutPrefix(d.body, "\r\n"); ok {
		return s
	}
	return strings.TrimPrefix(d.body, "\n")
}

// SetBody replaces the markdown after the frontmatter; a blank line is put
// between the two.
func (d *Document) SetBody(body string) {
	if body != "" && !strings.HasPrefix(body, "\n") {
		body = "\n" + body
	}
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	d.body = body
	d.raw = nil
}

// Keys lists the frontmatter keys in file order.
func (d *Document) Keys() []string {
	keys := make([]string, 0, len(d.front.Content)/2)
	for i := 0; i+1 < len(d.front.Content); i += 2 {
		keys = append(keys, d.front.Content[i].Value)
	}
	return keys
}

// Has reports whether the frontmatter has key.
func (d *Document) Has(key string) bool { return d.value(key) != nil }

// String returns the value of key when it is a scalar.
func (d *Document) String(key string) (string, bool) {
	v := d.value(key)
	if v == nil || v.Kind != yaml.ScalarNode {
		return "", false
	}
	return v.Value, true
}

// SetString sets key to a plain string, in place if the key is there.
func (d *Document) SetString(key, value string) { d.set(key, strNode(value)) }

// Delete removes key and reports whether it was there.
func (d *Document) Delete(key string) bool {
	for i := 0; i+1 < len(d.front.Content); i += 2 {
		if d.front.Content[i].Value == key {
			d.front.Content = slices.Delete(d.front.Content, i, i+2)
			d.raw = nil
			return true
		}
	}
	return false
}

// value returns the value node of key, or nil.
func (d *Document) value(key string) *yaml.Node {
	for i := 0; i+1 < len(d.front.Content); i += 2 {
		if d.front.Content[i].Value == key {
			return d.front.Content[i+1]
		}
	}
	return nil
}

// set replaces the value of key where it stands, or inserts the key where
// canonicalOrder puts it among the keys already there.
func (d *Document) set(key string, v *yaml.Node) {
	d.raw = nil
	for i := 0; i+1 < len(d.front.Content); i += 2 {
		if d.front.Content[i].Value == key {
			d.front.Content[i+1] = v
			return
		}
	}
	at := len(d.front.Content)
	rank := keyRank(key)
	for i := 0; i+1 < len(d.front.Content); i += 2 {
		if keyRank(d.front.Content[i].Value) > rank {
			at = i
			break
		}
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	d.front.Content = slices.Insert(d.front.Content, at, k, v)
}

// canonicalOrder is where new keys go: OKF's fields as the spec lists
// them, the Agent Skills fields a skill page also carries next to them.
var canonicalOrder = []string{
	KeyType, KeyTitle, KeyName, KeyDescription, KeyResource, KeyTags, KeyStatus,
	KeyGenerated, KeyVerified, KeyStaleAfter, KeySources, KeyUsageWindow,
	KeyLicense, KeyCompatibility, KeyMetadata, KeyAllowedTools,
}

func keyRank(key string) int {
	if i := slices.Index(canonicalOrder, key); i >= 0 {
		return i
	}
	return len(canonicalOrder)
}

var bom = []byte{0xEF, 0xBB, 0xBF} // a UTF-8 byte order mark

// split cuts a file into its frontmatter and body. The frontmatter opens
// with a line that is just "---" and closes with the next such line.
func split(data []byte) (front, body []byte, err error) {
	data = bytes.TrimPrefix(data, bom)
	end := lineEnd(data, 0)
	if !isFence(data[:end]) {
		return nil, nil, ErrNoFrontmatter
	}
	for i := end + 1; i < len(data); {
		e := lineEnd(data, i)
		if isFence(data[i:e]) {
			if e < len(data) {
				body = data[e+1:]
			}
			return data[end+1 : i], body, nil
		}
		i = e + 1
	}
	return nil, nil, fmt.Errorf("%w: it is not closed with ---", ErrBadFrontmatter)
}

func lineEnd(b []byte, from int) int {
	if i := bytes.IndexByte(b[from:], '\n'); i >= 0 {
		return from + i
	}
	return len(b)
}

func isFence(line []byte) bool { return string(bytes.TrimRight(line, " \t\r")) == "---" }

// parseMapping decodes frontmatter; empty frontmatter is an empty mapping.
func parseMapping(front []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(front, &doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadFrontmatter, err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, nil
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w: it is not a mapping of keys to values", ErrBadFrontmatter)
	}
	return m, nil
}

func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, child := range n.Content {
		c.Content[i] = cloneNode(child)
	}
	return &c
}
