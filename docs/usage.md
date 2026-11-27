# Using emby-mcp

Once the server is connected, just talk to your assistant about your library. This page shows what works well and explains how the tools behave, so you know what to expect. Most of it is about curation: tagging, collections and metadata, which is where an assistant saves the most time.

- [Example requests](#example-requests)
- [Example conversations](#example-conversations)
- [Built-in prompts](#built-in-prompts)
- [Finding things](#finding-things)
- [Tagging](#tagging)
- [Locks and metadata refreshes](#locks-and-metadata-refreshes)
- [Editing metadata](#editing-metadata)
- [Cast and crew after a save](#cast-and-crew-after-a-save)
- [Collections](#collections)
- [Playlists](#playlists)
- [Watched state and favourites](#watched-state-and-favourites)
- [Previewing a change](#previewing-a-change)
- [The raw API tool](#the-raw-api-tool)
- [Tips for good results](#tips-for-good-results)

## Example requests

**Finding something to watch**

- *"What's unwatched and under two hours?"* (`emby_search_items`)
- *"Something like Arrival that I haven't seen."* (`emby_get_similar`)
- *"What was added this week?"* (`emby_list_latest`)
- *"What am I halfway through?"* (`emby_list_resume`)

**Curating** (writes)

- *"Tag every Christmas film Christmas."* (`emby_add_tags`)
- *"Make a collection of every Christopher Nolan film."* (`emby_create_collection`)
- *"Merge the tag Sci Fi into Science Fiction."* (`emby_rename_tag`)
- *"Make a playlist of the Toy Story films in order."* (`emby_create_playlist`)

**Checking the library**

- *"Which films have no plot summary?"* (`emby_search_items` with `missing`)
- *"Which tags do I use, and how often?"* (`emby_list_values`)
- *"Which films didn't match on TMDb?"*

**The server**

- *"What's playing in the living room?"* (`emby_list_sessions`)
- *"Did last night's library scan finish?"* (`emby_list_scheduled_tasks`, `emby_list_activity`)
- *"Scan the films library for new files."* (writes)

## Example conversations

These show the kind of back-and-forth to expect. Your assistant's wording will differ.

### Tagging a theme

> **You:** *Tag every Christmas film Christmas.*
>
> **Assistant:** *You already use the tag "Christmas" on 4 films, and "Xmas" on 2. I read through the plots of your comedies and family films and found 19 more that clearly fit: Elf, Home Alone, The Holiday... (full list below). Shall I tag them, and merge "Xmas" into "Christmas" while I'm at it?*
>
> **You:** *Yes to both. Lock the tags too.*
>
> **Assistant:** *Done. 19 films are tagged Christmas, with the Tags field locked so a "replace all" refresh can't remove it, and the 2 "Xmas" films now have "Christmas" instead.*

Behind the scenes the assistant used `emby_list_values` (kind `tags`) to see the existing tags, `emby_search_items` with `include_overview` to judge which films fit, one `emby_add_tags` call with all 19 IDs and `lock: true`, and `emby_rename_tag`.

### A collection

> **You:** */mcp__emby__build_collection "Christopher Nolan films"*
>
> **Assistant:** *There's no Nolan collection yet. Your library has 9 of his films as director, from Following (1998) to Oppenheimer (2023). Here they are in release order. I'd call it "Christopher Nolan". Shall I create it?*
>
> **You:** *Go ahead.*
>
> **Assistant:** *Created "Christopher Nolan" with 9 films and a one-line description. Emby now shows them grouped under it.*

### Something to watch

> **You:** *We've got the kids tonight, one is nine. What's unwatched and under two hours?*
>
> **Assistant:** *Three good picks, all rated PG and unwatched: Paddington 2 (2017, 103 min, 7.8), The Iron Giant (1999, 86 min, 8.1) and Wallace & Gromit: The Curse of the Were-Rabbit (2005, 85 min, 7.4). I'd go for Paddington 2.*

## Built-in prompts

The server includes six prompts. Each one is a ready-made, multi-step request that tells the model which tools to use and how to report. Most clients show them as slash commands or in a prompt picker. In Claude Code they're `/mcp__emby__<name>`, for example `/mcp__emby__library_report`.

| Prompt | Arguments | What it does |
|---|---|---|
| `library_report` | `library` (optional) | A health check: what's in the library, the tag vocabulary and near-duplicates, how much is untagged, gaps in the metadata, small collections, and next steps. Doesn't change anything. |
| `tag_library` | `tags`, `type`, `library`, `batch` (all optional) | Agrees a tag vocabulary with you (or uses yours), then works through untagged films or series a batch at a time, showing each batch for approval before tagging it. |
| `build_collection` | `theme` (required), `name` | Finds everything that fits a theme, such as a director, franchise, mood or occasion, shows you the list, and creates the collection once you agree. |
| `suggest_collections` | `library` (optional) | Looks for film series, directors and themes that aren't grouped yet and proposes up to ten collections. With writes on, it creates the ones you pick. |
| `movie_night` | `mood`, `minutes`, `audience`, `user` (all optional) | Recommends three unwatched films for tonight, to suit a mood, the time you have and who's watching. Doesn't change anything. |
| `tidy_metadata` | `library` (optional) | Finds items with no TMDb match, no plot or no rating, groups the problems, and suggests fixes. With writes on, it refreshes or edits the items you agree to. |

`tag_library` and `build_collection` are only offered when writes are enabled, since that's their whole job. A prompt is also left out when a toolset it needs is turned off.

## Finding things

`emby_search_items` is the main finder. It combines any filters: text, item type, library, genres, tags (or `untagged: true`), years, parental rating, community and critic ratings, a person and their role, studio, watched or favourite, in progress, and missing metadata. Results are compact (ID, title, year, rating, runtime, genres, tags and watched state), sorted and paged: when there are more, the result has `next_offset` for the next call. `include_overview: true` adds a short plot summary, which the model needs to judge things like mood or theme.

Items are addressed by their Emby ID, which the search and list tools return. Tools that take one item, collection or playlist also accept its exact title. When a title is ambiguous, the error lists the matches with their IDs.

Watched state, favourites and resume points belong to one Emby user. Tools use the default user (`EMBY_USER`, or the first administrator) unless you name someone: *"What has Alex not watched yet?"*

`missing` finds gaps in the metadata. It takes any of `overview`, `tmdb_id`, `imdb_id`, `tvdb_id`, `official_rating`, `parental_rating`, `trailer` and `subtitles`. Films with no TMDb ID usually weren't matched at all, often because of the folder or file name.

## Tagging

Tags are Emby's free-form labels. They're shared by every user, show in Emby's filters, and can drive parental controls and smart views. emby-mcp's tag tools are built for tagging a whole library, not one item at a time.

- **Tag in bulk.** `emby_add_tags` takes up to 500 item IDs and up to 20 tags in one call. Items that already have a tag are skipped for it, and other tags are kept. The best way to tag a batch is to group the items by tag and make one call per tag: one call for every "Heist" film, one for every "Feel-good" film, and so on. The server tells the model to work this way.
- **Work through the library in batches.** `emby_search_items` with `untagged: true` returns only items with no tags at all. Tag a batch, then search again: the finished items drop out, so the next batch is whatever's left. The `tag_library` prompt works like this.
- **Let the model judge.** `include_overview: true` gives it each item's plot, so it can tell a Christmas film from a film released at Christmas. Ask it to tag only when it's confident.
- **Reuse the vocabulary.** `emby_list_values` with `kind: "tags"` and `with_counts: true` lists the tags in use, most-used first. Ask the assistant to check it before tagging, so it uses "Science Fiction" rather than inventing "Sci-Fi" alongside it.
- **Merge near-duplicates.** `emby_rename_tag` renames a tag on every item that has it, optionally only for some item types or one library. If the new name already exists, the items join it, so it doubles as a merge: *"Merge Xmas into Christmas."* It also fixes the case of a tag, such as "heist" to "Heist".
- **Remove tags.** `emby_remove_tags` takes the same up-to-500 item IDs.

Tags match without regard to case, as they do in Emby: "christmas" and "Christmas" are the same tag, and an item that has one is skipped when adding the other.

## Locks and metadata refreshes

Emby can refresh an item's metadata from the internet (TMDb, TVDb and others). A normal refresh only fills in what's missing. A **"replace all metadata"** refresh downloads everything again and overwrites every field that isn't locked, which can remove your tags and undo your edits.

Locked fields are left alone by every refresh. To keep your work:

- `emby_add_tags` with `lock: true` also locks each item's Tags field.
- `emby_update_item` locks the fields it edits, unless `lock_edited: false`. `lock_fields` and `unlock_fields` lock or unlock any of Emby's lockable fields, such as `Tags`, `Genres`, `Overview` or `Cast`, without editing them, and `lock_all` locks or unlocks the whole item.
- `emby_create_collection` with `lock: true` stops refreshes renaming the collection or changing its description.

`emby_refresh_item` runs a refresh for up to 500 items: `missing` (the default) fills in what's missing, `scan` only rescans the files, and `replace_all` overwrites every unlocked field, so it needs `confirm: true`. A series or collection refreshes its contents too. The refresh runs in the background on the server, so the new metadata appears a little later.

If you use "replace all" refreshes yourself in Emby, ask the assistant to lock what it tags.

## Editing metadata

`emby_update_item` edits one item as Emby's metadata editor does: title, sort title, original title, plot, tagline, genres, studios, parental and custom rating, community and critic rating, production year and premiere date. Only the fields you give change, and genres and studios replace the whole list. It works on collections and playlists too, for example to rename a collection or give it a description. Tags have their own tools, above.

The result lists the fields that changed. Before saving, the tool reads the whole item, since Emby replaces an item's metadata with what it's sent. Afterwards it reads the item back and says if anything that was there before is missing.

## Cast and crew after a save

When an item's metadata is saved, Emby matches its cast and crew by name, as its own metadata editor does. If two people share a name, one of them can end up linked to the other person: an actor's entry might then point to someone else's page and filmography.

This happens with `emby_update_item`, and with `emby_add_tags` when `lock: true` locks the Tags field (locking saves the whole item). The tool compares the cast and crew before and after the save, and if anyone was linked to a different person, its result names them. To link them by their TMDb ID again, refresh the item's metadata with `emby_refresh_item` and `mode: "replace_all"`. Lock the fields you want to keep first.

Plain tagging, without `lock`, doesn't save the whole item, so it isn't affected.

## Collections

A collection (Emby calls it a box set) groups films, or other items, under one entry in the library. Emby shows the films grouped under it, and `emby_search_items` with `group_into_collections: true` shows them the same way.

There are two kinds, and they work well together:

- **Film series from TMDb.** Emby's own library option **Import collections** (in the library's settings) makes collections such as "The Lord of the Rings Collection" automatically, from TMDb. `emby_list_libraries` shows whether it's on for each library, and the minimum number of films a collection needs. Leave franchises to Emby where you can.
- **Themed collections** that TMDb doesn't know about, such as "Christmas Classics", "Christopher Nolan" or "Best of Studio Ghibli". These are what emby-mcp is good at: `build_collection` and `suggest_collections` find the films and create them.

The tools:

- `emby_list_collections` lists the collections with how many items each holds, and `emby_get_collection` shows one in release order with each item's watched state.
- `emby_create_collection` creates a collection from up to 500 items, with an optional description. It refuses a name that's already a collection, unless `allow_duplicate: true`: add to the existing one instead.
- `emby_add_to_collection` and `emby_remove_from_collection` change what's in it. Removing an item only takes it out of the collection; it stays in the library.
- `emby_delete_collection` deletes the collection itself, never the films in it. It needs `EMBY_ALLOW_DELETES` and `confirm: true`.

## Playlists

Playlists are ordered lists, such as a film marathon or a run of episodes. Each belongs to a user: the default user unless you name someone.

- `emby_create_playlist` creates one from items in the order given. `media_type` is `Video` or `Audio`, worked out from the items when left out.
- `emby_add_to_playlist` adds items to the end, in order, skipping ones already in it unless `allow_repeats: true`. `emby_remove_from_playlist` takes every entry of the items given out of it.
- `emby_get_playlist` shows the items in order with the total running time.
- `emby_delete_playlist` deletes the playlist, never its items. It needs `EMBY_ALLOW_DELETES` and `confirm: true`.

## Watched state and favourites

Both are per user. `emby_set_played` marks up to 500 items watched or unwatched, optionally with the date watched. Marking a series or season marks all its episodes. `emby_set_favorite` adds items to a user's favourites or takes them out. `emby_list_users` shows the users and which one the tools use by default.

> *"Mark the whole of season 1 as watched for Alex."*

## Previewing a change

Every tool that changes something takes `dry_run: true`. The call is checked as usual (IDs, names, values, `confirm`) and reads what it needs from Emby, but nothing is changed. Instead you get the first request it would send, the current state of what it would change, a field-by-field list of the changes, and the plan: for a bulk change, every item and what would happen to it.

```json
{
  "dry_run": true,
  "would_send": {"method": "POST", "path": "/Items/102/Tags/Add", "body": {"Tags": [{"Name": "Christmas"}]}},
  "current": {"Tags": []},
  "changes": {"changed": {"Tags": {"from": [], "to": [{"Name": "Christmas"}]}}},
  "note": "The call would send 18 more requests after this one (see plan). Nothing was changed: this was a dry run. ...",
  "plan": {"done": true, "tags": ["Christmas"], "changed": 19, "already_tagged": 4, "items": [...]}
}
```

`would_send` also has `query` when the request has a query string. A call that wouldn't send anything returns `would_send: null` with its `result`.

Ask the assistant to "show me what you'd do first", or to do a dry run, before a big change. The server tells the model to use dry runs for big changes.

## The raw API tool

`emby_api_request` (toolset `raw`) calls any path of Emby's REST API, for anything the other tools don't cover. The paths are in your server's own API description at `/emby/openapi.json`. Without `EMBY_ALLOW_WRITES` it only allows `GET`. With writes on it allows `POST` too, with **no validation**, so it's marked destructive, and deletes, server restarts and changes to users, API keys, plugins, library folders and the server configuration also need `EMBY_ALLOW_DELETES`. It never deletes library items, since Emby would delete their media files. It isn't part of `EMBY_TOOLSETS=all` (the default): add it by name, for example `all,raw`. Access tokens, API keys and passwords are redacted from what it returns, and a very large response is cut short with a note to ask for less.

## Tips for good results

- **Agree the plan first.** For bulk changes, *"Show me the list first. Don't change anything yet."* then *"go ahead"*.
- **Settle the vocabulary.** A short, agreed list of tags works better than letting the model invent them as it goes. `library_report` shows what you have.
- **Batch it.** For a big library, tag 50 or 100 items at a time and check each batch. `tag_library` does this.
- **Say who's watching.** Watched state is per user, so *"for Alex"* matters for recommendations.
- **Lock what matters.** If you ever run "replace all" refreshes, ask for `lock: true` when tagging.
- **Fewer tools, better focus.** If the assistant picks the wrong tools, narrow `EMBY_TOOLSETS`.
