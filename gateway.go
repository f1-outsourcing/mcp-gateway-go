package main

import (
	"context"
	"flag"
	"log"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var version = "0.1"

func main() {
	var serverPath string
	var port string

	flag.StringVar(&serverPath, "s", "", "Path to stdio MCP server")
	flag.StringVar(&port, "p", "8080", "HTTP port")
	flag.Parse()

	if serverPath == "" {
		log.Fatal("missing -s server path")
	}

	ctx := context.Background()

	// -------------------------
	// Start stdio MCP client
	// -------------------------
	stdioClient, err := client.NewStdioMCPClient(serverPath, nil)
	if err != nil {
		log.Fatalf("failed to start stdio client: %v", err)
	}

	// Initialize (REQUIRED handshake)
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

	// -------------------------
	// List tools from stdio server
	// -------------------------
	toolsRes, err := stdioClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		log.Fatalf("list tools failed: %v", err)
	}

	log.Printf("exposing %d tools", len(toolsRes.Tools))

	// -------------------------
	// HTTP MCP server
	// -------------------------
	mcpServer := server.NewMCPServer(
		"stdio-http-gateway",
		version,
		server.WithToolCapabilities(true),
	)

	// -------------------------
	// Bridge tools stdio -> HTTP
	// -------------------------
	for _, tool := range toolsRes.Tools {
		t := tool

		mcpServer.AddTool(t, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {

			// Forward EXACT request format
			return stdioClient.CallTool(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      req.Params.Name,
					Arguments: req.Params.Arguments,
				},
			})
		})
	}

	// -------------------------
	// Start HTTP streaming server
	// -------------------------
	httpServer := server.NewStreamableHTTPServer(mcpServer)

	log.Printf("MCP gateway listening on :%s/mcp", port)
	log.Fatal(httpServer.Start(":" + port))
}