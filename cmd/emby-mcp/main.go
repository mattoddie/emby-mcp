// Command emby-mcp is a Model Context Protocol server for Emby media servers.
//
//	emby-mcp              serve MCP (stdio, or Streamable HTTP with MCP_TRANSPORT=http)
//	emby-mcp check        check the connection to Emby and the API key
//	emby-mcp healthcheck  check the HTTP server is up (for Docker HEALTHCHECK)
//	emby-mcp version      print the version
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mattoddie/emby-mcp/internal/config"
	"github.com/mattoddie/emby-mcp/internal/emby"
	"github.com/mattoddie/emby-mcp/internal/httpserver"
	"github.com/mattoddie/emby-mcp/internal/tools"
)

func main() {
	// Logs go to stderr: stdout is the MCP channel in stdio mode.
	log.SetFlags(0)
	log.SetOutput(os.Stderr)
	emby.UserAgent = "emby-mcp/" + tools.Version + " (+https://github.com/mattoddie/emby-mcp)"
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "check":
		err = check()
	case "healthcheck":
		err = healthcheck()
	case "version", "--version", "-v":
		fmt.Println(tools.Version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}
	if err != nil {
		log.Printf("emby-mcp: %v", err)
		os.Exit(1)
	}
}

const usage = `Usage: emby-mcp [command]

Commands:
  serve        Serve MCP (default). stdio, or Streamable HTTP with MCP_TRANSPORT=http
  check        Connect to Emby with EMBY_URL and EMBY_API_KEY and show the server, user and libraries
  healthcheck  Exit 0 if the HTTP server answers /health (for Docker HEALTHCHECK)
  version      Print the version

Configuration is by environment variable; see https://emby-mcp.mattoddie.dev/docs/configuration/
`

func load() (*config.Config, *emby.Client, error) {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return nil, nil, err
	}
	client, err := emby.New(cfg.Emby)
	if err != nil {
		return nil, nil, err
	}
	return cfg, client, nil
}

func serve() error {
	cfg, client, err := load()
	if err != nil {
		return err
	}
	writes := "disabled"
	if cfg.Tools.AllowWrites {
		writes = "ENABLED"
		if cfg.Tools.AllowDeletes {
			writes += " (with deletes)"
		}
	}
	if cfg.Tools.AuditLog != "" {
		writes += ", audit log " + cfg.Tools.AuditLog
	}
	log.Printf("emby-mcp %s: %s, writes %s, toolsets %d, transport %s", tools.Version, cfg.Emby.URL, writes, len(cfg.Tools.Toolsets), cfg.Transport)
	if w := cfg.Emby.Insecure(); w != "" {
		log.Printf("emby-mcp: note: %s", w)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server := tools.NewServer(client, cfg.Tools, nil)
	if cfg.Transport == "stdio" {
		err := server.Run(ctx, &mcp.StdioTransport{})
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	// No WriteTimeout: a tool call can legitimately take minutes.
	srv := &http.Server{
		Addr: httpserver.Addr(cfg.HTTP), Handler: httpserver.Handler(cfg.HTTP, server),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	return httpserver.Serve(srv, cfg.HTTP)
}

// check connects to Emby and reports what the server will see.
func check() error {
	cfg, client, err := load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var info emby.Obj
	if err := client.Get(ctx, "/System/Info", nil, &info); err != nil {
		return err
	}
	fmt.Printf("Connected to %s: Emby Server %s (%s).\n", emby.Str(info, "ServerName"), emby.Str(info, "Version"), cfg.Emby.URL)
	user, err := client.User(ctx, "")
	if err != nil {
		return fmt.Errorf("the API key works, but finding a user failed: %w", err)
	}
	how := "the first administrator; set EMBY_USER to choose"
	if cfg.Emby.User != "" {
		how = "EMBY_USER"
	}
	fmt.Printf("Acting as the user %s (%s).\n", emby.Str(user, "Name"), how)
	libs, err := client.Libraries(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%d libraries:\n", len(libs))
	for _, l := range libs {
		typ := emby.Str(l, "CollectionType")
		if typ == "" {
			typ = "mixed"
		}
		fmt.Printf("  - %s (%s)\n", emby.Str(l, "Name"), typ)
	}
	var counts emby.Obj
	if err := client.Get(ctx, "/Items/Counts", url.Values{"UserId": {emby.Str(user, "Id")}}, &counts); err == nil {
		fmt.Printf("%v films, %v series, %v episodes.\n", counts["MovieCount"], counts["SeriesCount"], counts["EpisodeCount"])
	}
	if w := cfg.Emby.Insecure(); w != "" {
		fmt.Printf("Note: %s\n", w)
	}
	switch {
	case cfg.Tools.AllowWrites && cfg.Tools.AllowDeletes:
		fmt.Println("Changes are allowed, including deleting collections and playlists.")
	case cfg.Tools.AllowWrites:
		fmt.Println("Changes are allowed (EMBY_ALLOW_WRITES=true).")
	default:
		fmt.Println("Read-only: set EMBY_ALLOW_WRITES=true to let the assistant tag, edit and build collections.")
	}
	return nil
}

// healthcheck asks the running server's /health, at the address and port it
// was configured with (including _FILE variables).
func healthcheck() error {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return err
	}
	if cfg.Transport == "stdio" {
		return nil
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(httpserver.HealthURL(cfg.HTTP))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned %s", resp.Status)
	}
	return nil
}
