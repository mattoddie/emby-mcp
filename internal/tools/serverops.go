package tools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mattoddie/emby-mcp/internal/emby"
)

func registerServer(c *Context) {
	cl := c.Client

	add(c, Def{
		Toolset: "server", Name: "emby_list_sessions", Title: "Now playing",
		Description: "Who's using Emby right now: each active session's user, app and device, and what's playing (title, position, paused, and whether it's direct playing or transcoding, with the reason). Idle sessions are left out unless include_idle is true.",
	}, func(ctx context.Context, a struct {
		IncludeIdle bool `json:"include_idle,omitempty" jsonschema:"Also list connected apps that aren't playing anything"`
	}) (any, error) {
		var sessions []Obj
		if err := cl.Get(ctx, "/Sessions", nil, &sessions); err != nil {
			return nil, err
		}
		var out []Obj
		for _, s := range sessions {
			np, playing := s["NowPlayingItem"].(Obj)
			if !playing && !a.IncludeIdle {
				continue
			}
			o := Obj{"user": emby.Str(s, "UserName"), "app": strings.TrimSpace(emby.Str(s, "Client") + " " + emby.Str(s, "ApplicationVersion")), "device": emby.Str(s, "DeviceName")}
			if v := emby.Str(s, "LastActivityDate"); v != "" {
				o["last_active"] = v
			}
			if playing {
				item := emby.Summary(np, false)
				ps, _ := s["PlayState"].(Obj)
				if pos, ok := emby.Num(ps, "PositionTicks"); ok {
					item["position_minutes"] = int(math.Round(pos / 600_000_000))
				}
				if emby.Bool(ps, "IsPaused") {
					item["paused"] = true
				}
				if m := emby.Str(ps, "PlayMethod"); m != "" {
					item["play_method"] = m
				}
				if ti, ok := s["TranscodingInfo"].(Obj); ok {
					t := Obj{}
					if r := emby.Strs(ti["TranscodeReasons"]); len(r) > 0 {
						t["reasons"] = r
					}
					for k, name := range map[string]string{"VideoCodec": "video_codec", "AudioCodec": "audio_codec", "Container": "container"} {
						if v := emby.Str(ti, k); v != "" {
							t[name] = v
						}
					}
					if v, ok := ti["IsVideoDirect"].(bool); ok {
						t["video_direct"] = v
					}
					item["transcoding"] = t
				}
				o["playing"] = item
			}
			out = append(out, o)
		}
		return listResult(out), nil
	})

	add(c, Def{
		Toolset: "server", Name: "emby_list_activity", Title: "Activity log",
		Description: "The server's activity log, newest first: playback started and stopped, sign-ins, new items added, scheduled task results, errors and warnings. Filter by date, severity or text.",
		Ranges:      map[string][2]float64{"limit": {1, 500}},
		Enums:       map[string][]string{"min_severity": {"Info", "Warning", "Error"}},
	}, func(ctx context.Context, a struct {
		Since       string `json:"since,omitempty" jsonschema:"Only entries from this date or time on (YYYY-MM-DD or RFC 3339)"`
		MinSeverity string `json:"min_severity,omitempty" jsonschema:"Only entries at least this severe"`
		Filter      string `json:"filter,omitempty" jsonschema:"Only entries whose name, type or description contains this"`
		Limit       int    `json:"limit,omitempty" jsonschema:"How many (default 50)"`
	}) (any, error) {
		limit := orInt(a.Limit, 50)
		q := url.Values{"Limit": {strconv.Itoa(500)}}
		if s := strings.TrimSpace(a.Since); s != "" {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				if t, err = time.ParseInLocation("2006-01-02", s, time.Local); err != nil {
					return nil, fmt.Errorf("since must be YYYY-MM-DD or RFC 3339, got %q", s)
				}
			}
			q.Set("MinDate", t.UTC().Format(time.RFC3339))
		}
		if a.MinSeverity == "" && a.Filter == "" {
			q.Set("Limit", strconv.Itoa(limit))
		}
		var res struct {
			Items            []Obj
			TotalRecordCount int
		}
		if err := cl.Get(ctx, "/System/ActivityLog/Entries", q, &res); err != nil {
			return nil, err
		}
		rank := map[string]int{"Info": 0, "Debug": 0, "Warn": 1, "Warning": 1, "Error": 2, "Fatal": 2}
		min := rank[a.MinSeverity]
		filter := strings.ToLower(strings.TrimSpace(a.Filter))
		var out []Obj
		for _, e := range res.Items {
			sev := emby.Str(e, "Severity")
			if rank[sev] < min {
				continue
			}
			text := emby.Str(e, "Name") + " " + emby.Str(e, "Type") + " " + emby.Str(e, "ShortOverview") + " " + emby.Str(e, "Overview")
			if filter != "" && !strings.Contains(strings.ToLower(text), filter) {
				continue
			}
			o := Obj{"date": emby.Str(e, "Date"), "name": emby.Str(e, "Name"), "type": emby.Str(e, "Type"), "severity": sev}
			if v := emby.Str(e, "ShortOverview"); v != "" {
				o["detail"] = v
			} else if v := emby.Str(e, "Overview"); v != "" {
				o["detail"] = clipText(v, 300)
			}
			out = append(out, o)
			if len(out) >= limit {
				break
			}
		}
		return listResult(out), nil
	})

	add(c, Def{
		Toolset: "server", Name: "emby_list_scheduled_tasks", Title: "Scheduled tasks",
		Description: "The server's scheduled tasks (library scan, metadata refresh, chapter images, backups from plugins...): each one's state and progress if running, and when it last ran and how that went. To start one, use its key or name.",
	}, func(ctx context.Context, a struct {
		IncludeHidden bool `json:"include_hidden,omitempty" jsonschema:"Include tasks Emby hides from its dashboard"`
	}) (any, error) {
		tasks, err := scheduledTasks(ctx, c, a.IncludeHidden)
		if err != nil {
			return nil, err
		}
		var out []Obj
		for _, t := range tasks {
			o := Obj{"name": emby.Str(t, "Name"), "key": emby.Str(t, "Key"), "id": emby.Str(t, "Id"), "category": emby.Str(t, "Category"), "state": emby.Str(t, "State")}
			if p, ok := emby.Num(t, "CurrentProgressPercentage"); ok {
				o["progress_percent"] = int(math.Round(p))
			}
			if r, ok := t["LastExecutionResult"].(Obj); ok {
				o["last_run"] = Obj{"status": emby.Str(r, "Status"), "ended": emby.Str(r, "EndTimeUtc")}
				if e := emby.Str(r, "ErrorMessage"); e != "" {
					o["last_run"].(Obj)["error"] = clipText(e, 300)
				}
			}
			out = append(out, o)
		}
		return listResult(out), nil
	})

	add(c, Def{
		Toolset: "server", Name: "emby_run_scheduled_task", Title: "Run a scheduled task",
		Description: "Start a scheduled task now, such as \"Scan media library\" or \"Refresh people\". Give its key, ID or name from emby_list_scheduled_tasks. It runs in the background.",
		Gate:        Write,
	}, func(ctx context.Context, a struct {
		Task string `json:"task" jsonschema:"The task's key, ID or name"`
	}) (any, error) {
		tasks, err := scheduledTasks(ctx, c, true)
		if err != nil {
			return nil, err
		}
		ref := strings.TrimSpace(a.Task)
		var pick Obj
		var names []string
		for _, t := range tasks {
			if emby.Str(t, "Id") == ref || strings.EqualFold(emby.Str(t, "Key"), ref) || strings.EqualFold(emby.Str(t, "Name"), ref) {
				pick = t
				break
			}
			names = append(names, emby.Str(t, "Name"))
		}
		if pick == nil {
			return nil, fmt.Errorf("no scheduled task %q; the tasks are %s", ref, strings.Join(names, ", "))
		}
		if emby.Str(pick, "State") == "Running" {
			return Obj{"done": true, "task": emby.Str(pick, "Name"), "note": "It's already running."}, nil
		}
		if err := emby.CheckID("task", emby.Str(pick, "Id")); err != nil {
			return nil, err
		}
		emby.SetCurrent(ctx, Obj{"State": emby.Str(pick, "State")})
		if err := cl.Post(ctx, "/ScheduledTasks/Running/"+emby.Str(pick, "Id"), nil, nil, nil); err != nil {
			return nil, err
		}
		return Obj{"done": true, "task": emby.Str(pick, "Name"), "note": "Started; follow it with emby_list_scheduled_tasks."}, nil
	})

	add(c, Def{
		Toolset: "server", Name: "emby_scan_library", Title: "Scan for new media",
		Description: "Scan for new, changed and removed files: every library, or one library. New films and episodes appear once the scan finds them. The scan runs in the background.",
		Gate:        Write,
	}, func(ctx context.Context, a struct {
		Library string `json:"library,omitempty" jsonschema:"Only this library (name or ID); default all"`
	}) (any, error) {
		if strings.TrimSpace(a.Library) == "" {
			if err := cl.Post(ctx, "/Library/Refresh", nil, nil, nil); err != nil {
				return nil, err
			}
			return Obj{"done": true, "scanning": "all libraries", "note": "The scan runs in the background; see emby_list_scheduled_tasks for progress."}, nil
		}
		lib, err := cl.Library(ctx, a.Library)
		if err != nil {
			return nil, err
		}
		id := emby.Str(lib, "ItemId")
		if err := emby.CheckID("library", id); err != nil {
			return nil, errors.New("couldn't find the library's ID")
		}
		q := url.Values{"Recursive": {"true"}, "MetadataRefreshMode": {"Default"}, "ImageRefreshMode": {"Default"}}
		if err := cl.Post(ctx, "/Items/"+id+"/Refresh", q, nil, nil); err != nil {
			return nil, err
		}
		return Obj{"done": true, "scanning": emby.Str(lib, "Name"), "note": "The scan runs in the background."}, nil
	})
}

func scheduledTasks(ctx context.Context, c *Context, hidden bool) ([]Obj, error) {
	q := url.Values{}
	if !hidden {
		q.Set("IsHidden", "false")
	}
	var tasks []Obj
	if err := c.Client.Get(ctx, "/ScheduledTasks", q, &tasks); err != nil {
		return nil, err
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := emby.Str(tasks[i], "Category")+emby.Str(tasks[i], "Name"), emby.Str(tasks[j], "Category")+emby.Str(tasks[j], "Name")
		return a < b
	})
	return tasks, nil
}

func clipText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
