// Package embytest is a small in-memory Emby server for tests. It answers the
// endpoints emby-mcp uses, from a library of made-up items, and records every
// request.
package embytest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// APIKey is the key the mock accepts.
const APIKey = "test-api-key-0123456789"

// AdminID and GuestID are the mock's users.
const (
	AdminID = "a1b2c3d4"
	GuestID = "e5f6a7b8"
)

// Request is one request the mock received.
type Request struct {
	Method, Path string
	Query        map[string][]string
	Body         string
}

// Server is the mock.
type Server struct {
	*httptest.Server
	mu       sync.Mutex
	Items    map[string]map[string]any
	order    []string
	nextID   int
	Requests []Request
	// Played and Favorite are per user: user ID → item ID → bool.
	Played   map[string]map[string]bool
	Favorite map[string]map[string]bool
	// Fail makes the given "METHOD /path" answer this status.
	Fail map[string]int
	// DropOnUpdate, when set, is a field POST /Items/{id} "loses", to test the safety checks.
	DropOnUpdate string
	// RelinkOnUpdate makes POST /Items/{id} link the first person to another
	// person with the same name, as Emby does when names are shared.
	RelinkOnUpdate bool
}

// New starts a mock with a small library.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{Items: map[string]map[string]any{}, nextID: 1000, Played: map[string]map[string]bool{AdminID: {}, GuestID: {}}, Favorite: map[string]map[string]bool{AdminID: {}, GuestID: {}}, Fail: map[string]int{}}
	s.seed()
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

func (s *Server) add(item map[string]any) string {
	id, _ := item["Id"].(string)
	if id == "" {
		s.nextID++
		id = strconv.Itoa(s.nextID)
		item["Id"] = id
	}
	for _, k := range []string{"Genres", "TagItems", "Studios", "People", "LockedFields", "Taglines"} {
		if item[k] == nil {
			item[k] = []any{}
		}
	}
	if item["ProviderIds"] == nil {
		item["ProviderIds"] = map[string]any{}
	}
	s.Items[id] = item
	s.order = append(s.order, id)
	return id
}

func tags(names ...string) []any {
	out := []any{}
	for i, n := range names {
		out = append(out, map[string]any{"Name": n, "Id": float64(500 + i)})
	}
	return out
}

func strs(v ...string) []any {
	out := []any{}
	for _, x := range v {
		out = append(out, x)
	}
	return out
}

// Movie IDs in the seeded library.
const (
	ElfID       = "101"
	HomeAloneID = "102"
	HeatID      = "103"
	ArrivalID   = "104"
	InceptionID = "105"
	NoMetaID    = "106"
	SeriesID    = "201"
	BoxSetID    = "301"
	PlaylistID  = "401"
	MoviesLib   = "9001"
	TVLib       = "9002"
)

func (s *Server) seed() {
	movie := func(id, name string, year int, rating string, community float64, minutes int, genres []any, tagItems []any, overview string) {
		s.add(map[string]any{"Id": id, "Name": name, "Type": "Movie", "MediaType": "Video", "ProductionYear": float64(year), "OfficialRating": rating,
			"CommunityRating": community, "RunTimeTicks": float64(minutes) * 600_000_000, "Genres": genres, "GenreItems": tags(), "TagItems": tagItems,
			"Overview": overview, "ParentId": MoviesLib, "ProviderIds": map[string]any{"Tmdb": "1" + id}, "Path": "/media/movies/" + name + "/" + name + ".mkv",
			"People":  []any{map[string]any{"Name": "A Director", "Type": "Director", "Id": "8" + id}, map[string]any{"Name": "An Actor", "Type": "Actor", "Role": "Lead", "Id": "9" + id}},
			"Studios": []any{map[string]any{"Name": "Studio " + name, "Id": float64(700)}}, "DateCreated": "2026-01-0" + id[2:] + "T10:00:00.0000000Z"})
	}
	movie(ElfID, "Elf", 2003, "PG", 7.0, 97, strs("Comedy", "Family"), tags("Christmas"), "A man raised by elves travels to New York to find his father.")
	movie(HomeAloneID, "Home Alone", 1990, "PG", 7.7, 103, strs("Comedy", "Family"), tags(), "A boy left home alone at Christmas defends the house from burglars.")
	movie(HeatID, "Heat", 1995, "15", 8.3, 170, strs("Crime", "Thriller"), tags(), "A detective hunts a crew of professional thieves.")
	movie(ArrivalID, "Arrival", 2016, "12A", 7.9, 116, strs("Science Fiction", "Drama"), tags("Mind-bending"), "A linguist works to talk with alien visitors.")
	movie(InceptionID, "Inception", 2010, "12A", 8.8, 148, strs("Science Fiction", "Action"), tags("mind-bending", "Heist"), "A thief who steals secrets through dreams is offered a last job.")
	s.add(map[string]any{"Id": NoMetaID, "Name": "home_video_2019", "Type": "Movie", "MediaType": "Video", "ParentId": MoviesLib, "Path": "/media/movies/home_video_2019.mp4"})
	s.add(map[string]any{"Id": SeriesID, "Name": "Space Station", "Type": "Series", "ProductionYear": float64(2020), "Genres": strs("Science Fiction"), "ParentId": TVLib, "ChildCount": float64(2), "ProviderIds": map[string]any{"Tvdb": "77"}, "Overview": "Life on a space station."})
	s.add(map[string]any{"Id": BoxSetID, "Name": "Christmas Classics", "Type": "BoxSet", "Overview": "Films for December.", "IsFolder": true})
	s.Items[ElfID]["BoxSets"] = []any{BoxSetID}
	s.add(map[string]any{"Id": PlaylistID, "Name": "Sci-fi marathon", "Type": "Playlist", "MediaType": "Video", "IsFolder": true, "Entries": []any{
		map[string]any{"ItemId": ArrivalID, "EntryId": "e1"}, map[string]any{"ItemId": InceptionID, "EntryId": "e2"}}})
	s.Played[AdminID][HeatID] = true
	s.Favorite[AdminID][ArrivalID] = true
}

var (
	userItemRe  = regexp.MustCompile(`^/Users/([^/]+)/Items/([^/]+)$`)
	userItemsRe = regexp.MustCompile(`^/Users/([^/]+)/Items(/Latest|/Resume)?$`)
	tagRe       = regexp.MustCompile(`^/Items/([^/]+)/Tags/(Add|Delete)$`)
	itemRe      = regexp.MustCompile(`^/Items/([^/]+)$`)
	refreshRe   = regexp.MustCompile(`^/Items/([^/]+)/Refresh$`)
	deleteRe    = regexp.MustCompile(`^/Items/([^/]+)/Delete$`)
	similarRe   = regexp.MustCompile(`^/Items/([^/]+)/Similar$`)
	colItemsRe  = regexp.MustCompile(`^/Collections/([^/]+)/Items(/Delete)?$`)
	plItemsRe   = regexp.MustCompile(`^/Playlists/([^/]+)/Items(/Delete)?$`)
	playedRe    = regexp.MustCompile(`^/Users/([^/]+)/(PlayedItems|FavoriteItems)/([^/]+)(/Delete)?$`)
	taskRe      = regexp.MustCompile(`^/ScheduledTasks/Running/([^/]+)$`)
)

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/emby")
	s.Requests = append(s.Requests, Request{Method: r.Method, Path: path, Query: r.URL.Query(), Body: string(body)})
	if r.Header.Get("X-Emby-Token") != APIKey {
		http.Error(w, "Access token is invalid or expired.", http.StatusUnauthorized)
		return
	}
	if code := s.Fail[r.Method+" "+path]; code != 0 {
		http.Error(w, "Simulated failure", code)
		return
	}
	q := r.URL.Query()
	reply := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	var m []string
	switch {
	case r.Method == "GET" && path == "/System/Info":
		reply(map[string]any{"ServerName": "Test Emby", "Version": "4.10.2.0", "OperatingSystemDisplayName": "Linux", "HasUpdateAvailable": false, "HasPendingRestart": false})
	case r.Method == "GET" && path == "/Users/Query":
		reply(map[string]any{"Items": []any{
			map[string]any{"Name": "Guest", "Id": GuestID, "Policy": map[string]any{"IsAdministrator": false, "MaxParentalRating": float64(10), "BlockedTags": []any{"Horror"}}, "LastActivityDate": "2026-11-20T10:00:00Z"},
			map[string]any{"Name": "Sam", "Id": AdminID, "Policy": map[string]any{"IsAdministrator": true}, "LastLoginDate": "2026-11-26T20:00:00Z"},
		}, "TotalRecordCount": 2})
	case r.Method == "GET" && path == "/Library/VirtualFolders":
		reply([]any{
			map[string]any{"Name": "Movies", "ItemId": MoviesLib, "CollectionType": "movies", "Locations": []any{"/media/movies"}, "LibraryOptions": map[string]any{"ImportCollections": true, "MinCollectionItems": float64(2), "SaveLocalMetadata": false, "PreferredMetadataLanguage": "en", "MetadataCountryCode": "GB"}},
			map[string]any{"Name": "TV", "ItemId": TVLib, "CollectionType": "tvshows", "Locations": []any{"/media/tv"}},
		})
	case r.Method == "GET" && path == "/Items/Counts":
		reply(map[string]any{"MovieCount": float64(6), "SeriesCount": float64(1), "EpisodeCount": float64(0), "BoxSetCount": float64(1)})
	case r.Method == "GET" && userItemRe.MatchString(path) && !strings.HasSuffix(path, "/Latest") && !strings.HasSuffix(path, "/Resume"):
		m = userItemRe.FindStringSubmatch(path)
		it, ok := s.Items[m[2]]
		if !ok {
			if _, err := strconv.Atoi(m[2]); err != nil {
				http.Error(w, "Unrecognized Guid format.", http.StatusInternalServerError) // as Emby does
				return
			}
			http.Error(w, "Item not found", http.StatusNotFound)
			return
		}
		reply(s.view(m[1], it, true))
	case r.Method == "GET" && userItemsRe.MatchString(path):
		m = userItemsRe.FindStringSubmatch(path)
		items := s.query(m[1], q)
		switch m[2] {
		case "/Latest":
			sort.SliceStable(items, func(i, j int) bool { return fmt.Sprint(items[i]["DateCreated"]) > fmt.Sprint(items[j]["DateCreated"]) })
			reply(items)
			return
		case "/Resume":
			reply(map[string]any{"Items": []any{}, "TotalRecordCount": 0})
			return
		}
		total := len(items)
		start, _ := strconv.Atoi(q.Get("StartIndex"))
		limit, err := strconv.Atoi(q.Get("Limit"))
		if err != nil {
			limit = total
		}
		if start > total {
			start = total
		}
		end := min(start+limit, total)
		out := []any{}
		for _, it := range items[start:end] {
			out = append(out, it)
		}
		reply(map[string]any{"Items": out, "TotalRecordCount": total})
	case r.Method == "POST" && tagRe.MatchString(path):
		m = tagRe.FindStringSubmatch(path)
		it, ok := s.Items[m[1]]
		if !ok {
			http.Error(w, "Item not found", http.StatusNotFound)
			return
		}
		var req struct{ Tags []struct{ Name string } }
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad body", 400)
			return
		}
		cur, _ := it["TagItems"].([]any)
		for _, t := range req.Tags {
			idx := -1
			for i, c := range cur {
				if strings.EqualFold(c.(map[string]any)["Name"].(string), t.Name) {
					idx = i
				}
			}
			if m[2] == "Add" && idx < 0 {
				cur = append(cur, map[string]any{"Name": t.Name, "Id": float64(600 + len(cur))})
			}
			if m[2] == "Delete" && idx >= 0 {
				cur = append(cur[:idx], cur[idx+1:]...)
			}
		}
		it["TagItems"] = cur
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "POST" && refreshRe.MatchString(path):
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "POST" && deleteRe.MatchString(path):
		m = deleteRe.FindStringSubmatch(path)
		delete(s.Items, m[1])
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "POST" && itemRe.MatchString(path):
		m = itemRe.FindStringSubmatch(path)
		it, ok := s.Items[m[1]]
		if !ok {
			http.Error(w, "Item not found", http.StatusNotFound)
			return
		}
		var upd map[string]any
		if err := json.Unmarshal(body, &upd); err != nil {
			http.Error(w, "bad body", 400)
			return
		}
		// Like Emby: the body replaces the editable metadata.
		for _, k := range []string{"Name", "SortName", "ForcedSortName", "OriginalTitle", "Overview", "Taglines", "Genres", "TagItems", "Studios", "People", "OfficialRating", "CustomRating", "CommunityRating", "CriticRating", "ProductionYear", "PremiereDate", "LockedFields", "LockData", "ProviderIds"} {
			if v, ok := upd[k]; ok {
				it[k] = v
			} else {
				delete(it, k)
			}
		}
		if s.DropOnUpdate != "" {
			it[s.DropOnUpdate] = []any{}
		}
		if people := asList(it["People"]); s.RelinkOnUpdate && len(people) > 0 {
			p := map[string]any{}
			for k, v := range people[0].(map[string]any) {
				p[k] = v
			}
			p["Id"] = "999999"
			it["People"] = append([]any{p}, people[1:]...)
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "GET" && similarRe.MatchString(path):
		m = similarRe.FindStringSubmatch(path)
		var out []any
		for _, id := range s.order {
			it := s.Items[id]
			if id != m[1] && it["Type"] == "Movie" && overlap(it["Genres"], s.Items[m[1]]["Genres"]) {
				out = append(out, s.view(q.Get("UserId"), it, false))
			}
		}
		reply(map[string]any{"Items": out})
	case r.Method == "POST" && path == "/Collections":
		id := s.add(map[string]any{"Name": q.Get("Name"), "Type": "BoxSet", "IsFolder": true})
		for _, x := range splitIDs(q.Get("Ids")) {
			s.addToBox(x, id)
		}
		reply(map[string]any{"Id": id})
	case r.Method == "POST" && colItemsRe.MatchString(path):
		m = colItemsRe.FindStringSubmatch(path)
		for _, x := range splitIDs(q.Get("Ids")) {
			if m[2] == "" {
				s.addToBox(x, m[1])
			} else if it := s.Items[x]; it != nil {
				var keep []any
				for _, b := range asList(it["BoxSets"]) {
					if b != m[1] {
						keep = append(keep, b)
					}
				}
				it["BoxSets"] = keep
			}
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "GET" && plItemsRe.MatchString(path):
		m = plItemsRe.FindStringSubmatch(path)
		pl := s.Items[m[1]]
		var out []any
		for _, e := range asList(pl["Entries"]) {
			em := e.(map[string]any)
			v := s.view(q.Get("UserId"), s.Items[em["ItemId"].(string)], false)
			v["PlaylistItemId"] = em["EntryId"]
			out = append(out, v)
		}
		reply(map[string]any{"Items": out, "TotalRecordCount": len(out)})
	case r.Method == "POST" && path == "/Playlists":
		var entries []any
		for i, x := range splitIDs(q.Get("Ids")) {
			entries = append(entries, map[string]any{"ItemId": x, "EntryId": fmt.Sprintf("n%d", i)})
		}
		id := s.add(map[string]any{"Name": q.Get("Name"), "Type": "Playlist", "MediaType": q.Get("MediaType"), "IsFolder": true, "Entries": entries})
		reply(map[string]any{"Id": id})
	case r.Method == "POST" && plItemsRe.MatchString(path):
		m = plItemsRe.FindStringSubmatch(path)
		pl := s.Items[m[1]]
		entries := asList(pl["Entries"])
		if m[2] == "" {
			for i, x := range splitIDs(q.Get("Ids")) {
				entries = append(entries, map[string]any{"ItemId": x, "EntryId": fmt.Sprintf("a%d-%d", len(entries), i)})
			}
		} else {
			drop := map[string]bool{}
			for _, e := range splitIDs(q.Get("EntryIds")) {
				drop[e] = true
			}
			var keep []any
			for _, e := range entries {
				if !drop[e.(map[string]any)["EntryId"].(string)] {
					keep = append(keep, e)
				}
			}
			entries = keep
		}
		pl["Entries"] = entries
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "POST" && playedRe.MatchString(path):
		m = playedRe.FindStringSubmatch(path)
		set := s.Played
		if m[2] == "FavoriteItems" {
			set = s.Favorite
		}
		if set[m[1]] == nil {
			set[m[1]] = map[string]bool{}
		}
		set[m[1]][m[3]] = m[4] == ""
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "GET" && path == "/Sessions":
		reply([]any{
			map[string]any{"UserName": "Sam", "Client": "Emby Theater", "ApplicationVersion": "4.8", "DeviceName": "Living room", "NowPlayingItem": s.view(AdminID, s.Items[HeatID], false),
				"PlayState":       map[string]any{"PositionTicks": float64(30 * 600_000_000), "IsPaused": false, "PlayMethod": "Transcode"},
				"TranscodingInfo": map[string]any{"TranscodeReasons": []any{"AudioCodecNotSupported"}, "IsVideoDirect": true, "AudioCodec": "aac"}},
			map[string]any{"UserName": "Guest", "Client": "Emby Web", "DeviceName": "Laptop"},
		})
	case r.Method == "GET" && path == "/System/ActivityLog/Entries":
		reply(map[string]any{"Items": []any{
			map[string]any{"Name": "Sam is playing Heat", "Type": "playback.start", "Severity": "Info", "Date": "2026-11-27T20:00:00Z"},
			map[string]any{"Name": "Scan media library failed", "Type": "ScheduledTaskFailed", "Severity": "Error", "Date": "2026-11-27T03:00:00Z", "ShortOverview": "Disk not ready"},
		}, "TotalRecordCount": 2})
	case r.Method == "GET" && path == "/ScheduledTasks":
		reply([]any{
			map[string]any{"Name": "Scan media library", "Key": "RefreshLibrary", "Id": "6330ee8fb4a957f33981f89aa78b030f", "Category": "Library", "State": "Idle", "LastExecutionResult": map[string]any{"Status": "Failed", "EndTimeUtc": "2026-11-27T03:00:00Z", "ErrorMessage": "Disk not ready"}},
			map[string]any{"Name": "Refresh people", "Key": "RefreshPeople", "Id": "8ad1c63c2a2b4b6a1e5b3a0e5c0e1f2a", "Category": "Library", "State": "Running", "CurrentProgressPercentage": 42.4},
		})
	case r.Method == "POST" && taskRe.MatchString(path), r.Method == "POST" && path == "/Library/Refresh":
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "GET" && (path == "/Tags" || path == "/Genres" || path == "/Studios" || path == "/Years"):
		seen := map[string]bool{}
		var out []any
		for _, id := range s.order {
			it := s.Items[id]
			var vals []string
			switch path {
			case "/Tags":
				for _, t := range asList(it["TagItems"]) {
					vals = append(vals, t.(map[string]any)["Name"].(string))
				}
			case "/Genres":
				for _, g := range asList(it["Genres"]) {
					vals = append(vals, g.(string))
				}
			}
			for _, v := range vals {
				if !seen[v] {
					seen[v] = true
					out = append(out, map[string]any{"Name": v})
				}
			}
		}
		reply(map[string]any{"Items": out})
	case r.Method == "GET" && path == "/OfficialRatings":
		reply([]any{map[string]any{"Name": "PG"}, map[string]any{"Name": "12A"}, map[string]any{"Name": "15"}})
	default:
		http.Error(w, "Not found: "+r.Method+" "+path, http.StatusNotFound)
	}
}

func (s *Server) addToBox(itemID, boxID string) {
	it := s.Items[itemID]
	if it == nil {
		return
	}
	for _, b := range asList(it["BoxSets"]) {
		if b == boxID {
			return
		}
	}
	it["BoxSets"] = append(asList(it["BoxSets"]), boxID)
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func splitIDs(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func overlap(a, b any) bool {
	for _, x := range asList(a) {
		for _, y := range asList(b) {
			if x == y {
				return true
			}
		}
	}
	return false
}

// view is an item as a user sees it, with UserData. full adds every field,
// as GET /Users/{id}/Items/{id} does; lists include only some.
func (s *Server) view(uid string, it map[string]any, full bool) map[string]any {
	out := map[string]any{}
	for k, v := range it {
		if k == "BoxSets" || k == "Entries" {
			continue
		}
		out[k] = v
	}
	if it["Type"] == "BoxSet" {
		n := 0
		for _, x := range s.Items {
			for _, b := range asList(x["BoxSets"]) {
				if b == it["Id"] {
					n++
				}
			}
		}
		out["ChildCount"] = float64(n)
	}
	if it["Type"] == "Playlist" {
		out["ChildCount"] = float64(len(asList(it["Entries"])))
	}
	if !full {
		delete(out, "People")
		delete(out, "LockedFields")
	}
	id, _ := it["Id"].(string)
	out["UserData"] = map[string]any{"Played": s.Played[uid][id], "IsFavorite": s.Favorite[uid][id]}
	return out
}

func lowerSet(pipe string) map[string]bool {
	out := map[string]bool{}
	for _, t := range strings.Split(pipe, "|") {
		if t = strings.TrimSpace(t); t != "" {
			out[strings.ToLower(t)] = true
		}
	}
	return out
}

// query filters the library like GET /Users/{id}/Items, for the parameters emby-mcp sends.
func (s *Server) query(uid string, q map[string][]string) []map[string]any {
	get := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	ids := map[string]bool{}
	for _, x := range splitIDs(get("Ids")) {
		ids[x] = true
	}
	types := map[string]bool{}
	for _, x := range splitIDs(get("IncludeItemTypes")) {
		types[x] = true
	}
	exclude := map[string]bool{}
	for _, x := range splitIDs(get("ExcludeItemTypes")) {
		exclude[x] = true
	}
	tagSet, exTags, genreSet := lowerSet(get("Tags")), lowerSet(get("ExcludeTags")), lowerSet(get("Genres"))
	years := map[string]bool{}
	for _, y := range splitIDs(get("Years")) {
		years[y] = true
	}
	term := strings.ToLower(get("SearchTerm"))
	parent := get("ParentId")
	listItem := get("ListItemIds")
	var out []map[string]any
	for _, id := range s.order {
		it, ok := s.Items[id]
		if !ok {
			continue
		}
		typ, _ := it["Type"].(string)
		if len(ids) > 0 && !ids[id] {
			continue
		}
		if len(types) > 0 && !types[typ] || exclude[typ] {
			continue
		}
		// Like Emby, an Ids query leaves out collections unless
		// IncludeItemTypes names BoxSet.
		if len(ids) > 0 && typ == "BoxSet" && !types["BoxSet"] {
			continue
		}
		if term != "" && !strings.Contains(strings.ToLower(it["Name"].(string)), term) {
			continue
		}
		if parent != "" {
			inBox := false
			for _, b := range asList(it["BoxSets"]) {
				inBox = inBox || b == parent
			}
			if it["ParentId"] != parent && !inBox {
				continue
			}
		}
		if listItem != "" {
			if typ != "BoxSet" {
				continue
			}
			member := false
			for _, b := range asList(s.Items[listItem]["BoxSets"]) {
				member = member || b == id
			}
			if !member {
				continue
			}
		}
		var itTags []string
		for _, t := range asList(it["TagItems"]) {
			itTags = append(itTags, strings.ToLower(t.(map[string]any)["Name"].(string)))
		}
		if len(tagSet) > 0 && !anyIn(itTags, tagSet) || anyIn(itTags, exTags) {
			continue
		}
		var genres []string
		for _, g := range asList(it["Genres"]) {
			genres = append(genres, strings.ToLower(g.(string)))
		}
		if len(genreSet) > 0 && !anyIn(genres, genreSet) {
			continue
		}
		if len(years) > 0 && !years[fmt.Sprint(it["ProductionYear"])] {
			continue
		}
		if p := get("IsPlayed"); p != "" && strconv.FormatBool(s.Played[uid][id]) != p {
			continue
		}
		if f := get("IsFavorite"); f != "" && strconv.FormatBool(s.Favorite[uid][id]) != f {
			continue
		}
		if get("HasOverview") == "false" && it["Overview"] != nil {
			continue
		}
		if get("HasTmdbId") == "false" && it["ProviderIds"].(map[string]any)["Tmdb"] != nil {
			continue
		}
		out = append(out, s.view(uid, it, false))
	}
	if strings.HasPrefix(get("SortBy"), "ProductionYear") {
		sort.SliceStable(out, func(i, j int) bool {
			a, _ := out[i]["ProductionYear"].(float64)
			b, _ := out[j]["ProductionYear"].(float64)
			if get("SortOrder") == "Descending" {
				return a > b
			}
			return a < b
		})
	}
	return out
}

func anyIn(list []string, set map[string]bool) bool {
	for _, x := range list {
		if set[x] {
			return true
		}
	}
	return false
}

// Calls returns the requests whose method and path match, such as "POST /Items/101/Tags/Add".
func (s *Server) Calls(methodPath string) []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Request
	for _, r := range s.Requests {
		if r.Method+" "+r.Path == methodPath {
			out = append(out, r)
		}
	}
	return out
}

// Writes counts the requests other than GET.
func (s *Server) Writes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.Requests {
		if r.Method != "GET" {
			n++
		}
	}
	return n
}

// TagNames returns an item's tag names.
func (s *Server) TagNames(id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, t := range asList(s.Items[id]["TagItems"]) {
		out = append(out, t.(map[string]any)["Name"].(string))
	}
	return out
}
