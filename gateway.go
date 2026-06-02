package main

import (
	"context"
	"flag"
	"log"
	"net/http"

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

	flag.StringVar(&serverPath, "s", "", "Path to stdio MCP server")
	flag.StringVar(&port, "p", "8080", "HTTP port")
	flag.StringVar(&transport, "t", "http", "http | sse")
	flag.StringVar(&baseURL, "baseUrl", "", "Public base URL for SSE (IMPORTANT for remote clients)")
	flag.Parse()

	if serverPath == "" {
		log.Fatal("missing -s server path")
	}

	if transport == "sse" && baseURL == "" {
		log.Println("WARNING: SSE mode without --baseUrl may break remote clients")
		log.Println("Example: --baseUrl http://192.168.3.11:8080")
	}

	ctx := context.Background()

	// -------------------------
	// Start stdio MCP client
	// -------------------------
	stdioClient, err := client.NewStdioMCPClient(serverPath, nil)
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

	// =========================
	// MODERN MCP HTTP (/mcp)
	// =========================
	case "http":
		httpServer := server.NewStreamableHTTPServer(mcpServer)

		log.Printf("MCP gateway (HTTP) on :%s/mcp", port)
		log.Fatal(httpServer.Start(":" + port))

	// =========================
	// LEGACY SSE (/sse + /message)
	// =========================
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