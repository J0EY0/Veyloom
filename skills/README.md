# Veyloom's own skills

Every agent has these skills, in every project, apart from the skills people install for agents from the skill library. They come with Veyloom: the binary carries this folder (`veyloom.Skills` in `embed.go`), and the hub hands them to every turn, the way it hands the installed ones.

Each skill is a folder laid out as [Agent Skills](https://agentskills.io) has it: `<name>/SKILL.md`, whose frontmatter gives the same `name` as the folder and a `description` saying when to use it, and any files it refers to. Only text files travel. The skill library refuses a skill with the name of one of these.

- `team-practices`: how members split, hand on, check and report back work.
