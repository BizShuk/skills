package plugin

import "github.com/bizshuk/skills/model"

// Category is one node in the plugin tree. Skills and Subagents are the
// direct leaves of this node; Children are nested remote plugins fetched
// during discovery and surfaced as sub-trees. The OwnerRepo is empty for
// the root of the walk and for purely local categories; remote plugins
// carry the lowercased "owner/repo" so the TUI can render provenance and
// so the visited set can dedupe repeats.
//
// FetchOK is true when either the local scan succeeded (Locals case) or
// the remote fetch returned a usable directory. FetchErr is non-empty
// only when FetchOK is false; the TUI surfaces its content next to the
// plugin name.
type Category struct {
	PluginName string
	OwnerRepo  string
	Skills     []model.Skill
	Subagents  []model.Subagent
	Children   []*Category // sub-plugins (empty for leaf nodes)
	FetchOK    bool
	FetchErr   string
}

// Catalog is the result of Walk: an ordered list of root-level plugins.
// Root-level plugins are what the user's source points to; remote plugins
// fetched during the walk become Children of their parent, not roots.
type Catalog struct {
	Roots []*Category
}

// AllSkills walks the tree in preorder and returns every Skill — leaf or
// nested — flattened into one slice. The TUI's --yes (non-interactive)
// branch and any other consumer that wants "every skill" without caring
// about the tree shape use this helper.
func (c *Catalog) AllSkills() []model.Skill {
	if c == nil {
		return nil
	}
	var out []model.Skill
	var walk func(n *Category)
	walk = func(n *Category) {
		if n == nil {
			return
		}
		out = append(out, n.Skills...)
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	for _, r := range c.Roots {
		walk(r)
	}
	return out
}

// AllSubagents walks the tree in preorder and returns every Subagent
// flattened into one slice. Consumers that want "every subagent" without
// caring about the tree shape (e.g. --yes mode) use this helper.
func (c *Catalog) AllSubagents() []model.Subagent {
	if c == nil {
		return nil
	}
	var out []model.Subagent
	var walk func(n *Category)
	walk = func(n *Category) {
		if n == nil {
			return
		}
		out = append(out, n.Subagents...)
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	for _, r := range c.Roots {
		walk(r)
	}
	return out
}


