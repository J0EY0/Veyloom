package okf

import (
	"bytes"
	"path"
	"strings"
)

// A skill's folder holds, besides its SKILL.md, whatever its author put
// there: references, templates, notes. The markdown among them is theirs,
// not written to this package's rules: kept in a bundle, each file stays
// the file it was, a type line put in front of it when it has none, which
// is all OKF asks of a concept (docs/design.md 5.11, 5.13).

// SkillFolderFile reports whether p, a path in a bundle, is a file of a
// skill's folder other than the skill's own SKILL.md: /skills/<name>/...
func SkillFolderFile(p string) bool {
	rest, ok := strings.CutPrefix(path.Clean("/"+p), "/skills/")
	if !ok {
		return false
	}
	name, inner, ok := strings.Cut(rest, "/")
	return ok && name != "" && inner != "" && inner != SkillFile
}

// WithType returns data, a markdown file, as a concept of type typ: as it
// is when its frontmatter has a type; with a "type: <typ>" line opening its
// frontmatter when that has none; else, when it has no frontmatter that
// reads, after a frontmatter of that line alone. WithoutType undoes it.
func WithType(data []byte, typ string) []byte {
	line := KeyType + ": " + typ
	d, err := Parse(data)
	switch {
	case err == nil && d.Type() != "":
		return data
	case err == nil && !d.Has(KeyType) && !bytes.HasPrefix(data, bom):
		end := bytes.IndexByte(data, '\n')
		eol := "\n"
		if end > 0 && data[end-1] == '\r' {
			eol = "\r\n"
		}
		out := append([]byte(nil), data[:end+1]...)
		out = append(out, line+eol...)
		return append(out, data[end+1:]...)
	}
	return append([]byte("---\n"+line+"\n---\n"), data...)
}

// WithoutType returns the file that WithType made data of, as a skill
// carries it: without the type line when the type is typ, and without what
// a bundle's writers stamp on a concept, who wrote it last and who checked
// it. A file nothing was stamped on or changed in comes back to the byte;
// one written since, by an agent improving the skill say, keeps its own
// keys and its text, the frontmatter written anew.
func WithoutType(data []byte, typ string) []byte {
	d, err := Parse(data)
	if err != nil || d.Type() == "" {
		return data
	}
	if !d.Has(KeyGenerated) && !d.Has(KeyVerified) {
		block := "---\n" + KeyType + ": " + typ + "\n---\n"
		if bytes.HasPrefix(data, []byte(block)) {
			return data[len(block):]
		}
		first := bytes.IndexByte(data, '\n')
		second := -1
		if first >= 0 {
			second = bytes.IndexByte(data[first+1:], '\n')
		}
		if second >= 0 && !bytes.HasPrefix(data, bom) {
			line := data[first+1 : first+1+second]
			if string(bytes.TrimSuffix(line, []byte("\r"))) == KeyType+": "+typ {
				out := append([]byte(nil), data[:first+1]...)
				return append(out, data[first+1+second+1:]...)
			}
		}
	}
	if t, _ := d.String(KeyType); t == typ {
		d.Delete(KeyType)
	}
	d.Delete(KeyGenerated)
	d.Delete(KeyVerified)
	if len(d.Keys()) == 0 {
		return []byte(d.body)
	}
	out, err := d.Bytes()
	if err != nil {
		return data
	}
	return out
}
