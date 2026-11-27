package emby

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// ListFields are the extra fields item lists ask Emby for.
const ListFields = "Genres,Tags,TagItems,ProductionYear,OfficialRating,CommunityRating,CriticRating,DateCreated,ChildCount,RecursiveItemCount,PremiereDate"

// ticksPerMinute converts Emby's RunTimeTicks (100 ns units) to minutes.
const ticksPerMinute = 600_000_000

// Tags returns an item's tags, from TagItems or Tags.
func Tags(item Obj) []string {
	if t := Names(item["TagItems"]); len(t) > 0 {
		return t
	}
	t := Strs(item["Tags"])
	if t == nil {
		t = []string{}
	}
	return t
}

// Genres returns an item's genres.
func Genres(item Obj) []string {
	g := Strs(item["Genres"])
	if len(g) == 0 {
		g = Names(item["GenreItems"])
	}
	if g == nil {
		g = []string{}
	}
	return g
}

// Minutes is an item's running time in whole minutes, or 0.
func Minutes(item Obj) int {
	t, ok := Num(item, "RunTimeTicks")
	if !ok || t <= 0 {
		return 0
	}
	return int(math.Round(t / ticksPerMinute))
}

// Year is the production year, or the premiere year.
func Year(item Obj) int {
	if y, ok := Num(item, "ProductionYear"); ok && y > 0 {
		return int(y)
	}
	if t, err := time.Parse(time.RFC3339, Str(item, "PremiereDate")); err == nil {
		return t.Year()
	}
	return 0
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

// Summary is the compact form of an item used in lists.
func Summary(item Obj, overview bool) Obj {
	s := Obj{"id": Str(item, "Id"), "name": Str(item, "Name"), "type": Str(item, "Type")}
	if y := Year(item); y > 0 {
		s["year"] = y
	}
	if v := Str(item, "SeriesName"); v != "" {
		s["series"] = v
	}
	if Str(item, "Type") == "Episode" {
		if n, ok := Num(item, "ParentIndexNumber"); ok {
			s["season"] = int(n)
		}
		if n, ok := Num(item, "IndexNumber"); ok {
			s["episode"] = int(n)
		}
	}
	if v := Str(item, "OfficialRating"); v != "" {
		s["rating"] = v
	}
	if v, ok := Num(item, "CommunityRating"); ok && v > 0 {
		s["community_rating"] = round1(v)
	}
	if v, ok := Num(item, "CriticRating"); ok && v > 0 {
		s["critic_rating"] = v
	}
	if m := Minutes(item); m > 0 {
		s["minutes"] = m
	}
	if g := Genres(item); len(g) > 0 {
		s["genres"] = g
	}
	if _, has := item["TagItems"]; has || item["Tags"] != nil {
		s["tags"] = Tags(item)
	}
	switch Str(item, "Type") {
	case "BoxSet", "Playlist", "Series", "Season", "Folder", "CollectionFolder", "MusicAlbum":
		if n, ok := Num(item, "ChildCount"); ok {
			s["items"] = int(n)
		} else if n, ok := Num(item, "RecursiveItemCount"); ok {
			s["items"] = int(n)
		}
	}
	if ud, ok := item["UserData"].(Obj); ok {
		if Bool(ud, "Played") {
			s["played"] = true
		}
		if Bool(ud, "IsFavorite") {
			s["favorite"] = true
		}
		if p, ok := Num(ud, "PlayedPercentage"); ok && p > 0 && !Bool(ud, "Played") {
			s["progress_percent"] = int(math.Round(p))
		}
		if n, ok := Num(ud, "UnplayedItemCount"); ok && n > 0 {
			s["unplayed_items"] = int(n)
		}
	}
	if v := Str(item, "PlaylistItemId"); v != "" {
		s["playlist_entry_id"] = v
	}
	if overview {
		if v := strings.TrimSpace(Str(item, "Overview")); v != "" {
			s["overview"] = clip(v, 400)
		}
	}
	return s
}

// clip shortens s to about n bytes at a word boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := strings.LastIndexByte(s[:n], ' ')
	if cut < n/2 {
		cut = n
	}
	return strings.TrimRight(s[:cut], " ,.;:") + "…"
}

// Detail is the full, readable form of one item.
func Detail(item Obj) Obj {
	d := Summary(item, false)
	if v := Str(item, "OriginalTitle"); v != "" && v != Str(item, "Name") {
		d["original_title"] = v
	}
	if v := Str(item, "SortName"); v != "" && v != Str(item, "Name") {
		d["sort_name"] = v
	}
	if v := Str(item, "PremiereDate"); v != "" {
		d["premiere_date"] = strings.SplitN(v, "T", 2)[0]
	}
	if v := Str(item, "CustomRating"); v != "" {
		d["custom_rating"] = v
	}
	if v := strings.TrimSpace(Str(item, "Overview")); v != "" {
		d["overview"] = v
	}
	if t := Strs(item["Taglines"]); len(t) > 0 {
		d["tagline"] = t[0]
	}
	d["tags"] = Tags(item)
	d["genres"] = Genres(item)
	if s := Names(item["Studios"]); len(s) > 0 {
		d["studios"] = s
	}
	if c := Names(item["Collections"]); len(c) > 0 {
		d["collections"] = c
	}
	people := Obj{}
	var cast []string
	for _, p := range Objs(item["People"]) {
		name, role := Str(p, "Name"), Str(p, "Role")
		switch Str(p, "Type") {
		case "Actor", "GuestStar":
			if len(cast) < 12 {
				if role != "" {
					name += " (" + role + ")"
				}
				cast = append(cast, name)
			}
		case "Director", "Writer", "Producer", "Composer":
			k := strings.ToLower(Str(p, "Type")) + "s"
			list, _ := people[k].([]string)
			if len(list) < 6 {
				people[k] = append(list, name)
			}
		}
	}
	if len(cast) > 0 {
		people["cast"] = cast
	}
	if len(people) > 0 {
		d["people"] = people
	}
	if ids, ok := item["ProviderIds"].(Obj); ok && len(ids) > 0 {
		d["provider_ids"] = ids
	}
	if v := Str(item, "DateCreated"); v != "" {
		d["date_added"] = strings.SplitN(v, "T", 2)[0]
	}
	if v := Str(item, "Path"); v != "" {
		d["path"] = v
	}
	if lf := Strs(item["LockedFields"]); len(lf) > 0 {
		d["locked_fields"] = lf
	}
	if Bool(item, "LockData") {
		d["locked"] = true
	}
	if m := media(item); len(m) > 0 {
		d["media"] = m
	}
	if ud, ok := item["UserData"].(Obj); ok {
		u := Obj{"played": Bool(ud, "Played"), "favorite": Bool(ud, "IsFavorite")}
		if n, ok := Num(ud, "PlayCount"); ok && n > 0 {
			u["play_count"] = int(n)
		}
		if v := Str(ud, "LastPlayedDate"); v != "" {
			u["last_played"] = v
		}
		if p, ok := Num(ud, "PlayedPercentage"); ok && p > 0 {
			u["progress_percent"] = int(math.Round(p))
		}
		d["user_data"] = u
	}
	return d
}

// media summarises the first media source: file, size, video and audio streams, subtitles.
func media(item Obj) Obj {
	sources := Objs(item["MediaSources"])
	if len(sources) == 0 {
		return nil
	}
	src := sources[0]
	m := Obj{}
	if v := Str(src, "Container"); v != "" {
		m["container"] = v
	}
	if v, ok := Num(src, "Size"); ok && v > 0 {
		m["size_gb"] = math.Round(v/1e7) / 100
	}
	if v, ok := Num(src, "Bitrate"); ok && v > 0 {
		m["bitrate_mbps"] = round1(v / 1e6)
	}
	var audio, subs []string
	for _, st := range Objs(src["MediaStreams"]) {
		switch Str(st, "Type") {
		case "Video":
			if _, done := m["video"]; done {
				continue
			}
			v := Str(st, "DisplayTitle")
			if v == "" {
				v = Str(st, "Codec")
			}
			if w, ok := Num(st, "Width"); ok {
				h, _ := Num(st, "Height")
				m["resolution"] = strconv.Itoa(int(w)) + "x" + strconv.Itoa(int(h))
			}
			if r := Str(st, "VideoRange"); r != "" && r != "SDR" {
				m["hdr"] = r
			}
			m["video"] = v
		case "Audio":
			if t := Str(st, "DisplayTitle"); t != "" && len(audio) < 8 {
				audio = append(audio, t)
			}
		case "Subtitle":
			if t := Str(st, "DisplayTitle"); t != "" && len(subs) < 12 {
				subs = append(subs, t)
			}
		}
	}
	if len(audio) > 0 {
		m["audio"] = audio
	}
	if len(subs) > 0 {
		m["subtitles"] = subs
	}
	if len(sources) > 1 {
		m["versions"] = len(sources)
	}
	return m
}
