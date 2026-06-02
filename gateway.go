package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os/exec"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var version = "0.1"

func main() {
	var (
		serverPath string
		port       string
		transport  string
		baseURL    string
	)

	flag.StringVar(&serverPath, "s", "", "Path to stdio MCP server (with optional args)")
	flag.StringVar(&port, "p", "8080", "HTTP port")
	flag.StringVar(&transport, "t", "http", "http | sse")
	flag.StringVar(&baseURL, "baseUrl", "", "Public base URL for SSE")
	flag.Parse()

	if serverPath == "" {
		log.Fatal("missing -s server path")
	}

	if transport == "sse" && baseURL == "" {
		log.Println("WARNING: SSE mode without --baseUrl may break remote clients")
	}

	ctx := context.Background()

	// -------------------------
	// Split serverPath into executable + args
	// -------------------------
	parts := strings.Fields(serverPath)
	cmdName := parts[0]
	cmdArgs := parts[1:]

	cmd := exec.Command(cmdName, cmdArgs...)
	stdioClient, err := client.NewStdioMCPClient(cmd.Path, cmdArgs)
	if err != nil {
		log.Fatalf("failed to start stdio client: %v", err)
	}

	_, err = stdioClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "mcp-gateway",
				Version: version,
			},
		},
	})
	if err != nil {
		log.Fatalf("initialize failed: %v", err)
	}

	toolsRes, err := stdioClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		log.Fatalf("list tools failed: %v", err)
	}

	log.Printf("exposing %d tools", len(toolsRes.Tools))

	// -------------------------
	// MCP server (shared)
	// -------------------------
	mcpServer := server.NewMCPServer(
		"stdio-http-gateway",
		version,
		server.WithToolCapabilities(true),
	)

	for _, tool := range toolsRes.Tools {
		t := tool

		mcpServer.AddTool(t, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return stdioClient.CallTool(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      req.Params.Name,
					Arguments: req.Params.Arguments,
				},
			})
		})
	}

	// -------------------------
	// Transport switch
	// -------------------------
	switch transport {
	case "http":
		httpServer := server.NewStreamableHTTPServer(mcpServer)
		log.Printf("MCP gateway (HTTP) on :%s/mcp", port)
		log.Fatal(httpServer.Start(":" + port))

	case "sse":
		if baseURL == "" {
			baseURL = "http://localhost:" + port
		}

		sseServer := server.NewSSEServer(
			mcpServer,
			server.WithSSEEndpoint("/sse"),
			server.WithMessageEndpoint("/message"),
			server.WithBaseURL(baseURL),
		)

		mux := http.NewServeMux()
		mux.Handle("/sse", sseServer.SSEHandler())
		mux.Handle("/message", sseServer.MessageHandler())

		log.Printf("MCP gateway (SSE) on :%s", port)
		log.Printf("  baseUrl=%s", baseURL)
		log.Println("  /sse + /message enabled")

		log.Fatal(http.ListenAndServe(":"+port, mux))

	default:
		log.Fatalf("unknown transport: %s (use http or sse)", transport)
	}
}