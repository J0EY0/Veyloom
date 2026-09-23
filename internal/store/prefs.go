package store

// MemoryPrefs say which memories the members' turns use (docs/design.md
// 5.19): a switch for memory as a whole, and one for each of the two
// memories under it. Turning one off leaves what it holds as it is: it is
// only not used. They are the account's, kept with it in the state dir,
// not in the database.
type MemoryPrefs struct {
	Enabled  bool `json:"enabled"`
	Personal bool `json:"personal"`
	Project  bool `json:"project"`
}

// DefaultMemoryPrefs are how a Veyloom starts: every memory on.
var DefaultMemoryPrefs = MemoryPrefs{Enabled: true, Personal: true, Project: true}

// UsesPersonal reports whether turns use the personal memory, the one
// every project shares.
func (p MemoryPrefs) UsesPersonal() bool { return p.Enabled && p.Personal }

// UsesProject reports whether turns use their project's memory.
func (p MemoryPrefs) UsesProject() bool { return p.Enabled && p.Project }

// UsesAny reports whether turns use a memory at all.
func (p MemoryPrefs) UsesAny() bool { return p.UsesPersonal() || p.UsesProject() }
