package runtime

// The wiki tools are how an agent reads and writes its project's wiki
// (design.md 5.5): the decisions, conventions, facts, pitfalls, module
// notes and topic write-ups the team keeps; and, with scope library, the
// skill library every project shares (5.10): patterns and skills. They
// travel the same way as the room tools and end in TurnHost.QueryRoom too,
// but their arguments go to the hub as they came: what they mean is the
// hub's business, where the wikis live. Searching and reading change
// nothing; the three that write are Veyloom's own, so every preset has
// them, and the hub decides who may change what.
const (
	WikiToolSearch    = "search_wiki"
	WikiToolRead      = "read_wiki"
	WikiToolRelated   = "related_wiki"
	WikiToolWrite     = "write_wiki"
	WikiToolPatch     = "patch_wiki"
	WikiToolDeprecate = "deprecate_wiki"
)

// WikiToolNames lists the wiki tools in the order they are presented.
var WikiToolNames = []string{WikiToolSearch, WikiToolRead, WikiToolRelated, WikiToolWrite, WikiToolPatch, WikiToolDeprecate}

// WikiPageTypes are the kinds of page a project wiki holds, as write_wiki
// offers them; each has a directory of its own (design.md 5.3).
var WikiPageTypes = []string{"Decision", "Convention", "Fact", "Pitfall", "Module", "Topic"}

// LibraryPageTypes are the kinds of page the skill library holds (5.10).
// Agents write patterns there; skills come from people (5.15).
var LibraryPageTypes = []string{"Pattern", "Skill"}

// Scopes of the wiki tools: the project's wiki, or the skill library.
const (
	WikiScopeProject = "project"
	WikiScopeLibrary = "library"
)

var (
	paramWikiPath = roomToolParam{
		Name: "path", Type: "string", Required: true,
		Description: "The page's path from the wiki's root, like /decisions/approvals-payload-json.md or /skills/go-table-tests/SKILL.md, as search_wiki or a link gives it.",
	}
	paramWikiScope = roomToolParam{
		Name: "scope", Type: "string", Enum: []string{WikiScopeProject, WikiScopeLibrary},
		Description: "Which wiki: project, the default, is this project's; library is the skill library every project shares, its patterns and skills.",
	}
)

// wikiToolSpecs are the wiki tools as an agent sees them.
var wikiToolSpecs = []roomToolSpec{
	{
		Name: WikiToolSearch, ReadOnly: true, RawArgs: true,
		Description: "Search this project's wiki: the decisions, conventions, facts, pitfalls, module notes and topic write-ups the team has recorded. " +
			"With scope library, search the skill library instead: patterns of how tasks go wrong or right, and the skills made of them. " +
			"Each word is matched as written, ignoring case, in titles, descriptions, tags, paths and text; the best matches come first. Read a page with read_wiki.",
		Params: []roomToolParam{
			{Name: "query", Type: "string", Description: "The words to look for: names, paths, identifiers or error text work as well as plain words. Words are split at spaces only, so give Chinese as short words with spaces between (审批 超时), not as a whole question.", Required: true},
			{Name: "limit", Type: "integer", Description: "How many pages to show at most."},
			paramWikiScope,
		},
	},
	{
		Name: WikiToolRead, ReadOnly: true, RawArgs: true,
		Description: "Read one page of this project's wiki as it is stored: its YAML frontmatter (type, title, description, sources, who wrote and who confirmed it) " +
			"and its markdown text, then the pages that link to it. To change the page, copy the text you want to change from here into patch_wiki.",
		Params: []roomToolParam{paramWikiPath, paramWikiScope},
	},
	{
		Name: WikiToolRelated, ReadOnly: true, RawArgs: true,
		Description: "See how the pages of this project's wiki bear on each other. Give a page's path to list the pages one step from it, or two with depth 2, " +
			"each with how it relates: the sentence one links to the other in, which says what the relation is; which supersedes which; which rests on which as a source; " +
			"the paths of the repository both name; the topics both came from. " +
			"Give a path of the repository instead, a file or a directory, to list the pages that name it and the pages one step from those: " +
			"before you change a file, the decisions, conventions and pitfalls about it. With scope library, the same from a page of the skill library; " +
			"it is shared by every project, so it takes no path of the repository. Read a page with read_wiki.",
		Params: []roomToolParam{
			{Name: "path", Type: "string", Description: "A page's path from the wiki's root, like /decisions/approvals-payload-json.md."},
			{Name: "file", Type: "string", Description: "A path of the repository, a file or a directory, like internal/hub/brief.go. Give this or path."},
			{Name: "depth", Type: "integer", Description: "How many steps out to go: 1, the default, or 2."},
			paramWikiScope,
		},
	},
	{
		Name: WikiToolWrite, RawArgs: true,
		Description: "Add a page to this project's wiki, for what should outlast this conversation: a decision and why it was made, an agreed convention, " +
			"a fact you checked, a pitfall you hit and the way around it, what a module is for, or what a finished topic came to. " +
			"Search first, and change an existing page with patch_wiki rather than writing a second one. " +
			"What you write is saved at once, whatever its kind, and a person can undo it. " +
			"Link to other pages by their path from the wiki's root, like [the payload decision](/decisions/approvals-payload-json.md). " +
			"With scope library, write a Pattern to the skill library every project shares, for what holds beyond this repository: " +
			"one way tasks go wrong or right, with the symptom, the root cause, the exact commands and the fix, linking to the skill it is about, if any, by its path (/skills/<name>/SKILL.md). " +
			"A Pattern's description says the problem, its root cause and the fix in one line, so that the list of patterns tells whether one bears on a task without opening it. It is saved at once. " +
			"Skills themselves are added by people, who install them for agents.",
		Params: []roomToolParam{
			{Name: "type", Type: "string", Required: true, Description: "What kind of page it is: Pattern is the library's.", Enum: append(append([]string(nil), WikiPageTypes...), "Pattern")},
			{Name: "slug", Type: "string", Required: true, Description: "The page's file name: a few lowercase English words joined by hyphens, like approvals-payload-json."},
			{Name: "title", Type: "string", Required: true, Description: "A short title, in the language the team works in."},
			{Name: "description", Type: "string", Required: true, Description: "One sentence saying what the page says; listings and search results show it."},
			{Name: "body", Type: "string", Required: true, Description: "The page's markdown. To cite a source for a claim, add a footnote named after the source's id, like [^m1], and define it at the end."},
			{Name: "tags", Description: "A few short words to group the page with others.", Schema: map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
			paramWikiSources,
			{Name: "topics", Description: "Topics of this chat the page draws on, by number: 12 for #12. The turn you are in is recorded for you.", Schema: map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}},
			{Name: "files", Description: "Files people sent in this chat to keep with the page as they are, by the file id read_topic or read_room gives: " +
				"a picture, a spec, a data file worth having whole, up to 10 MB each. The page links them; say in it what they hold. Project wiki only.",
				Schema: map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
			paramWikiScope,
		},
	},
	{
		Name: WikiToolPatch, RawArgs: true,
		Description: "Change a page of this project's wiki by editing its text, frontmatter included: append to the end, replace an exact piece of text, or insert after one. " +
			"Copy the text to find from read_wiki exactly; if it no longer matches, the page has changed, so read it again. " +
			"Changes are saved at once, and a person can undo them. " +
			"With scope library you may change a skill installed for you, or, as the wiki maintainer, one your team owns: its SKILL.md or a page of its folder. " +
			"Read the skill with read_wiki first: it names the changes to it that were rolled back, and why; do not make one of them again. " +
			"The change reaches every agent the skill is installed for from its next turn, on trial until enough turns have used it. " +
			"Calling it is how a change is made: one only described in your reply reaches no one.",
		Params: []roomToolParam{
			paramWikiPath,
			{Name: "edits", Required: true, Description: "The edits, applied in order.", Schema: map[string]any{
				"type": "array", "minItems": 1,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"op":      map[string]any{"type": "string", "enum": []string{"append", "replace", "insert_after"}},
						"target":  map[string]any{"type": "string", "description": "The exact text to replace or to insert after; not used by append."},
						"content": map[string]any{"type": "string", "description": "The new text."},
					},
					"required":             []string{"op", "content"},
					"additionalProperties": false,
				},
			}},
			paramWikiReason,
			paramWikiScope,
		},
	},
	{
		Name: WikiToolDeprecate, RawArgs: true,
		Description: "Mark a page of this project's wiki as no longer true or no longer followed. It stays for its links and history, marked as such; " +
			"name the page that takes over when there is one. It takes effect at once, and a person can undo it. A skill itself is retired by people, not with this tool.",
		Params: []roomToolParam{
			paramWikiPath,
			{Name: "successor", Type: "string", Description: "The path of the page that takes over, if any."},
			{Name: "reason", Type: "string", Required: true, Description: "Why it no longer holds, in a sentence or two."},
			paramWikiScope,
		},
	},
}

var (
	paramWikiSources = roomToolParam{Name: "sources", Description: "What the page rests on besides this chat: web pages or other wiki pages.", Schema: map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "description": "A short name for footnotes to cite, like m1."},
				"resource": map[string]any{"type": "string", "description": "A URL, or a wiki page's path from the root like /facts/go-version.md."},
				"title":    map[string]any{"type": "string"},
			},
			"required":             []string{"resource"},
			"additionalProperties": false,
		},
	}}
	paramWikiReason = roomToolParam{Name: "reason", Type: "string", Description: "Why, in a sentence: it goes into the wiki's history beside the change, where people and later upkeeps read it."}
)
