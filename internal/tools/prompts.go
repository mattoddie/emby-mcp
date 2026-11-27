package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type prompt struct {
	name, title, description string
	args                     []*mcp.PromptArgument
	// needs lists the tools the prompt relies on: it's only offered when
	// they're all registered, so a read-only server has no prompts that change things.
	needs []string
	// text renders the prompt; has reports whether a tool is registered.
	text func(args map[string]string, has func(tool string) bool) string
}

// libArg renders an optional library argument for a tool call in a prompt.
func or(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func libArg(a map[string]string) string {
	if l := strings.TrimSpace(a["library"]); l != "" {
		return fmt.Sprintf(", library: %q", l)
	}
	return ""
}

var prompts = []prompt{
	{
		name: "library_report", title: "Library report",
		description: "A health check of the Emby library: what's in it, how well it's tagged and organised, gaps in the metadata, and suggestions.",
		needs:       []string{"emby_get_server_info", "emby_search_items", "emby_list_values"},
		args:        []*mcp.PromptArgument{{Name: "library", Description: "Only this library (default all of them)"}},
		text: func(a map[string]string, has func(string) bool) string {
			scope := "the whole library"
			if l := strings.TrimSpace(a["library"]); l != "" {
				scope = "the " + l + " library"
			}
			lib := libArg(a)
			s := fmt.Sprintf(`Give me a report on %s in Emby.
1. emby_get_server_info for the libraries and counts.
2. emby_list_values kind tags with_counts: true (types ["Movie"]%s), and the same for genres, to see how the library is tagged.
3. emby_search_items types ["Movie"], untagged: true, limit 1%s: how many films have no tags.
4. Gaps: emby_search_items with missing ["overview"], then ["tmdb_id"], then ["official_rating"] (limit 10 each%s), and the same for types ["Series"].
`, scope, lib, lib, lib)
			if has("emby_list_collections") {
				s += "5. emby_list_collections: how many collections, and which are tiny (one or two items).\n"
			}
			return s + `Report: a short summary with counts; the tag vocabulary (most used, rarely used, and near-duplicates such as "Sci-Fi" and "Science Fiction" that could be merged); how much is untagged; the metadata gaps with examples; and three to five concrete next steps. Don't change anything.`
		},
	},
	{
		name: "tag_library", title: "Tag the library",
		description: "Go through films (or series) and tag them by theme, mood or occasion, a batch at a time, with your approval before anything is saved.",
		needs:       []string{"emby_search_items", "emby_list_values", "emby_add_tags"},
		args: []*mcp.PromptArgument{
			{Name: "tags", Description: "Tags to use, comma-separated (default: suggest a vocabulary first)"},
			{Name: "type", Description: "Movie (default) or Series"},
			{Name: "library", Description: "Only this library"},
			{Name: "batch", Description: "How many items to look at per batch (default 50)"},
		},
		text: func(a map[string]string, _ func(string) bool) string {
			typ := or(a["type"], "Movie")
			lib := libArg(a)
			vocab := "Then suggest a vocabulary of 15 to 30 tags (themes, moods and occasions people actually browse by, such as Christmas, Heist, Feel-good, Date night, Mind-bending, Based on a true story, Kids and Halloween), reusing existing tags where they fit, and wait for me to approve or edit it."
			if t := strings.TrimSpace(a["tags"]); t != "" {
				vocab = fmt.Sprintf("Use exactly these tags: %s. Reuse the existing spelling where one matches.", t)
			}
			return fmt.Sprintf(`Help me tag my %s items in Emby.
1. emby_list_values kind tags with_counts: true, types [%q]%s, to see the tags already in use. %s
2. Work in batches: emby_search_items types [%q]%s, untagged: true, include_overview: true, limit %s. For each item, choose the tags that clearly fit from its title, year, genres and overview, plus what you know of it. Tag an item only when you're confident, and give it at most four tags.
3. Show the batch as a table (title, year, proposed tags) and wait for my go-ahead or corrections.
4. Apply the batch grouped by tag: one emby_add_tags call per tag with all the item IDs that get it (with lock: true if I want the tags to survive a "replace all" metadata refresh). Report what changed.
5. Move on to the next batch (search with untagged: true again, so finished items drop out) until I say stop or nothing untagged is left. Keep a running total.`, typ, typ, lib, vocab, typ, lib, or(a["batch"], "50"))
		},
	},
	{
		name: "build_collection", title: "Build a collection",
		description: "Find everything in the library that fits a theme (a franchise, a director, a mood or an occasion) and make it into an Emby collection.",
		needs:       []string{"emby_search_items", "emby_create_collection"},
		args: []*mcp.PromptArgument{
			{Name: "theme", Description: "What the collection is, such as \"Christopher Nolan films\" or \"cosy winter films\"", Required: true},
			{Name: "name", Description: "The collection's name (default: suggest one)"},
		},
		text: func(a map[string]string, _ func(string) bool) string {
			name := "Suggest a short name for it."
			if n := strings.TrimSpace(a["name"]); n != "" {
				name = fmt.Sprintf("Call it %q.", n)
			}
			return fmt.Sprintf(`Make an Emby collection of %s. %s
1. emby_list_collections with a key word as query, in case a similar collection exists; if one does, offer to add to it instead.
2. Find candidates with emby_search_items: person (with person_types such as ["Director"]) for people, tags and genres for themes, query for franchises, and year_from/year_to for eras. Use include_overview: true when the theme needs judgement, and emby_get_similar to find ones the searches missed.
3. Show the list in release order (title, year, and one line on why it fits), plus near misses I might want, and wait for my go-ahead or edits.
4. emby_create_collection with the agreed item IDs and a one-sentence description (overview). Report the collection's ID and size.`, a["theme"], name)
		},
	},
	{
		name: "suggest_collections", title: "Suggest collections",
		description: "Look through the library for sets worth making: film series, directors and themes that aren't grouped yet.",
		needs:       []string{"emby_search_items", "emby_list_collections", "emby_list_values"},
		args:        []*mcp.PromptArgument{{Name: "library", Description: "Only this library"}},
		text: func(a map[string]string, has func(string) bool) string {
			lib := libArg(a)
			s := fmt.Sprintf(`Suggest new collections for my Emby films.
1. emby_list_collections: what already exists.
2. emby_search_items types ["Movie"]%s, sorted by name, limit 500 (page with offset if there are more): read through the titles and years.
3. Look for film series and franchises that aren't a collection yet (or are missing entries), directors with four or more films, and strong themes or occasions (Christmas, Halloween, Pixar, Bond...). Check emby_list_values kind tags with_counts: true for themes already tagged.
4. Propose up to ten collections in a table: name, why, how many films, and which ones.
`, lib)
			if has("emby_create_collection") {
				return s + "5. Ask which I want. For each one I pick, create it with emby_create_collection and the item IDs, then report what was made. Don't create anything I haven't picked."
			}
			return s + "Don't change anything: creating collections needs EMBY_ALLOW_WRITES on this server."
		},
	},
	{
		name: "movie_night", title: "Pick something to watch",
		description: "Recommend a few unwatched films from the library for tonight, to suit a mood, the time you have and who's watching.",
		needs:       []string{"emby_search_items", "emby_list_resume"},
		args: []*mcp.PromptArgument{
			{Name: "mood", Description: "What you're in the mood for, such as \"funny\", \"tense\" or \"something like Arrival\""},
			{Name: "minutes", Description: "The most time you have, in minutes"},
			{Name: "audience", Description: "Who's watching, such as \"family with a 9-year-old\" or \"just me\""},
			{Name: "user", Description: "Whose watched history to use (Emby user name)"},
		},
		text: func(a map[string]string, _ func(string) bool) string {
			user := ""
			if u := strings.TrimSpace(a["user"]); u != "" {
				user = fmt.Sprintf(", user: %q", u)
			}
			return fmt.Sprintf(`Pick something for me to watch tonight from my Emby library.
Mood: %s. Time: %s. Audience: %s.
1. emby_list_resume%s: is there something I'm part-way through that fits?
2. emby_search_items types ["Movie"], played: false, include_overview: true%s, with filters that fit: genres or tags for the mood, max_rating for the audience, and sort_by community_rating (descending) or random. If I named a film I like, use emby_get_similar on it. Leave out anything longer than the time I have.
3. Recommend three, each with its year, runtime, rating and one line on why it suits tonight, and say which you'd pick. Don't change anything.`, or(a["mood"], "anything good"), or(a["minutes"], "no limit"), or(a["audience"], "adults"), user, user)
		},
	},
	{
		name: "tidy_metadata", title: "Tidy up metadata",
		description: "Find items with missing or wrong metadata (no plot, no TMDb match, no rating) and fix them by refreshing or editing, with your approval.",
		needs:       []string{"emby_search_items", "emby_get_item"},
		args:        []*mcp.PromptArgument{{Name: "library", Description: "Only this library"}},
		text: func(a map[string]string, has func(string) bool) string {
			s := fmt.Sprintf(`Help me tidy up the metadata in my Emby library.
1. emby_search_items types ["Movie","Series"] with missing ["tmdb_id"]%s, then missing ["overview"], then missing ["official_rating"] (limit 50 each). Items with no TMDb or TVDb ID usually weren't matched at all, often because of the folder or file name.
2. For a few examples, emby_get_item to see the path and what's there.
3. Group the problems: unmatched items (with their file paths and the likely correct title and year), items with partial metadata, and anything else odd (such as wrong years or titles).
`, libArg(a))
			if has("emby_refresh_item") && has("emby_update_item") {
				return s + `4. Propose fixes: emby_refresh_item (mode missing) for partial metadata, emby_update_item for small corrections such as a title, year or rating, and for unmatched items, the folder renames I should make myself ("Title (Year)"). Wait for my go-ahead, then apply what I agree to and report.`
			}
			return s + `4. Suggest fixes I can make myself in Emby or on disk. Don't change anything: edits need EMBY_ALLOW_WRITES on this server.`
		},
	},
}

// registerPrompts adds the prompts whose tools are all registered.
func registerPrompts(c *Context) {
	for _, p := range prompts {
		p := p
		missing := false
		for _, t := range p.needs {
			missing = missing || !c.tools[t]
		}
		if missing {
			continue
		}
		c.Server.AddPrompt(&mcp.Prompt{Name: p.name, Title: p.title, Description: p.description, Arguments: p.args},
			func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				args := map[string]string{}
				if req.Params != nil {
					args = req.Params.Arguments
				}
				return &mcp.GetPromptResult{
					Description: p.description,
					Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: p.text(args, func(t string) bool { return c.tools[t] })}}},
				}, nil
			})
	}
}

// PromptNames lists the prompts in the order they are registered.
func PromptNames() []string {
	names := make([]string, len(prompts))
	for i, p := range prompts {
		names[i] = p.name
	}
	return names
}
