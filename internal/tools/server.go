package tools

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mattoddie/emby-mcp/internal/config"
	"github.com/mattoddie/emby-mcp/internal/emby"
)

// Name is the MCP server name.
const Name = "emby-mcp"

// Version is set at build time with -ldflags "-X github.com/mattoddie/emby-mcp/internal/tools.Version=1.2.3".
var Version = "0.0.0-dev"

func instructions(opts config.Tools) string {
	s := "Tools for an Emby media server: search and browse the libraries (films, TV, music, home videos), read an item's full metadata, see what's new, in progress or playing now, and curate the library. " +
		"Start with emby_get_server_info for the libraries and item counts. Items are addressed by their Emby ID, which search and list tools return; never guess an ID. " +
		"emby_search_items is the main finder: filter by text, type, library, genre, tag, year, rating, person, studio, played state and missing metadata, and page with offset. List tools return {total, returned, items}. " +
		"Played state, favourites and resume points belong to one Emby user: tools use the default user unless given user. "
	if opts.AllowWrites {
		s += "Write tools are enabled and change the real library, which everyone in the household sees: show the user what you plan (which items, which tags or collection) and get their agreement before bulk changes. " +
			"Tags and collections are the curation tools: emby_add_tags and emby_remove_tags take many item IDs at once, so group items by tag and make one call per tag rather than one per item. " +
			"To build a set, use emby_create_collection with the item IDs. Pass lock: true when tagging if the user refreshes metadata with 'replace all', so their tags survive. " +
			"Every tool that changes something accepts dry_run: true, which checks the call and returns the plan and the first request it would send, without changing anything; use it to show the user a big change first. "
		if opts.AllowDeletes {
			s += "Delete tools only remove collections and playlists (never media files), and need confirm: true. "
		}
	} else {
		s += "The server is read-only; tools that tag, edit metadata, build collections or playlists, or mark items played are disabled (EMBY_ALLOW_WRITES=false). "
	}
	return s
}

// NewServer creates the MCP server with the tools and prompts allowed by opts.
// registered, when non-nil, is filled with each tool's toolset and gate.
func NewServer(client *emby.Client, opts config.Tools, registered map[string]Info) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: Name, Version: Version}, &mcp.ServerOptions{Instructions: instructions(opts)})
	c := &Context{Server: s, Client: client, Tools: opts, Registered: registered, audited: map[string]func(json.RawMessage) bool{}, tools: map[string]bool{}}
	if audit := newAuditLog(opts.AuditLog); audit != nil {
		s.AddReceivingMiddleware(audit.middleware(c.audited))
	}
	registerLibrary(c)
	registerMetadata(c)
	registerCollections(c)
	registerPlaylists(c)
	registerUsers(c)
	registerServer(c)
	registerRaw(c)
	registerPrompts(c)
	return s
}
