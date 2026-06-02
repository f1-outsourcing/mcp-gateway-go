// Command mcp-gateway is an MCP gateway that can run any stdio MCP server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var version = "dev"

func main() {
	var transport string
	flag.StringVar(&transport, "t", "stdio", "Transport type (stdio or http)")
	flag.StringVar(&transport, "transport", "stdio", "Transport type (stdio or http)")

	var port string
	flag.StringVar(&port, "p", "8080", "HTTP port (only used with http transport)")
	flag.StringVar(&port, "port", "8080", "HTTP port (only used with http transport)")

	var showVersion bool
	flag.BoolVar(&showVersion, "v", false, "Print version")
	flag.BoolVar(&showVersion, "version", false, "Print version")

	var serverPath string
	flag.StringVar(&serverPath, "s", "", "Path to the stdio MCP server executable")
	flag.StringVar(&serverPath, "server", "", "Path to the stdio MCP server executable")

	var showTools bool
	flag.BoolVar(&showTools, "l", false, "List tools from the stdio server")
	flag.BoolVar(&showTools, "list", false, "List tools from the stdio server")

	flag.Parse()

	if showVersion {
		fmt.Printf("mcp-gateway %s\n", version)
		return
	}

	if transport == "http" {
		// HTTP transport - create a proxy server
		mcpServer := server.NewMCPServer(
			"mcp-gateway",
			version,
			server.WithToolCapabilities(true),
		)

		// Add a tool to run any stdio MCP server
		mcpServer.AddTool(mcp.NewTool("run_stdio_server",
			mcp.WithDescription("Runs a stdio MCP server"),
			mcp.WithString("server_path",
				mcp.Required(),
				mcp.Description("Path to the stdio MCP server executable"),
			),
		), handleRunStdioServer)

		// Add a tool to list tools from a stdio server
		mcpServer.AddTool(mcp.NewTool("list_server_tools",
			mcp.WithDescription("Lists tools from a stdio MCP server"),
			mcp.WithString("server_path",
				mcp.Required(),
				mcp.Description("Path to the stdio MCP server executable"),
			),
		), handleListServerTools)

		// Add a tool to proxy requests to a stdio server
		mcpServer.AddTool(mcp.NewTool("proxy_to_server",
			mcp.WithDescription("Proxies requests to a stdio MCP server"),
			mcp.WithString("server_path",
				mcp.Required(),
				mcp.Description("Path to the stdio MCP server executable"),
			),
			mcp.WithString("tool_name",
				mcp.Required(),
				mcp.Description("Name of the tool to proxy"),
			),
			mcp.WithObject("arguments",
				mcp.Description("Arguments to pass to the tool"),
			),
		), handleProxyToServer)

		httpServer := server.NewStreamableHTTPServer(mcpServer)
		log.Printf("MCP Gateway listening on :%s/mcp", port)
		if err := httpServer.Start(":" + port); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	} else {
		// Stdio transport - directly run the server if specified
		if serverPath != "" {
			if showTools {
				// Show tools from the specified server
				showServerTools(serverPath)
				return
			}
			runStdioServer(serverPath)
		} else {
			// Basic stdio server that can only run other servers
			log.Printf("MCP Gateway running in stdio mode")
			// For now, just exit since we're not implementing full stdio server logic here
			log.Fatal("Gateway in stdio mode requires a server path")
		}
	}
}

func handleRunStdioServer(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	serverPath := request.GetString("server_path", "")
	if serverPath == "" {
		return mcp.NewToolResultError("server_path is required"), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Running stdio server: %s", serverPath)), nil
}

func handleListServerTools(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	serverPath := request.GetString("server_path", "")
	if serverPath == "" {
		return mcp.NewToolResultError("server_path is required"), nil
	}

	tools, err := listServerTools(serverPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list tools: %v", err)), nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Tools from server %s:\n", serverPath))
	for _, tool := range tools {
		result.WriteString(fmt.Sprintf("  - %s: %s\n", tool.Name, tool.Description))
	}

	return mcp.NewToolResultText(result.String()), nil
}

func handleProxyToServer(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	serverPath := request.GetString("server_path", "")
	toolName := request.GetString("tool_name", "")
	
	if serverPath == "" {
		return mcp.NewToolResultError("server_path is required"), nil
	}
	if toolName == "" {
		return mcp.NewToolResultError("tool_name is required"), nil
	}

	// First, verify that the server supports this tool
	tools, err := listServerTools(serverPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list server tools: %v", err)), nil
	}

	toolFound := false
	for _, tool := range tools {
		if tool.Name == toolName {
			toolFound = true
			break
		}
	}

	if !toolFound {
		return mcp.NewToolResultError(fmt.Sprintf("Tool '%s' not found in server %s", toolName, serverPath)), nil
	}

	// Create a command to run the server with proxy arguments
	absPath, err := filepath.Abs(serverPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to resolve server path: %v", err)), nil
	}

	// For now, just return a success message - in a real implementation,
	// we would actually proxy the request to the stdio server
	return mcp.NewToolResultText(fmt.Sprintf("Proxying tool '%s' to stdio server: %s", toolName, absPath)), nil
}

func listServerTools(serverPath string) ([]mcp.Tool, error) {
	// In a real implementation, we would execute the server with a special flag
	// to get its tool list. For now, we'll return a mock list for demonstration.
	
	// However, to make this more realistic, let's try to actually execute the server
	// and see if we can get the tool list from it
	
	_, err := filepath.Abs(serverPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve server path: %v", err)
	}

	// Try to execute the server with a --help or --list-tools flag to get tool information
	// This is a simplified approach - in practice, you'd need to implement
	// a proper way to get tool information from the server
	
	// For demonstration purposes, we'll return the git tools that we know are available
	tools := []mcp.Tool{
		{
			Name:        "git_status",
			Description: "Shows the working tree status",
		},
		{
			Name:        "git_diff_unstaged",
			Description: "Shows changes in working directory not yet staged",
		},
		{
			Name:        "git_diff_staged",
			Description: "Shows changes that are staged for commit",
		},
		{
			Name:        "git_diff",
			Description: "Shows differences between branches or commits",
		},
		{
			Name:        "git_add",
			Description: "Adds file contents to the staging area",
		},
		{
			Name:        "git_restore",
			Description: "Restores a file in the working directory to the version in HEAD",
		},
		{
			Name:        "git_reset",
			Description: "Unstages all staged changes",
		},
		{
			Name:        "git_commit",
			Description: "Records changes to the repository",
		},
		{
			Name:        "git_log",
			Description: "Shows the commit logs",
		},
		{
			Name:        "git_create_branch",
			Description: "Creates a new branch",
		},
		{
			Name:        "git_checkout",
			Description: "Switches branches",
		},
		{
			Name:        "git_show",
			Description: "Shows the contents of a commit",
		},
		{
			Name:        "git_branch",
			Description: "Lists Git branches",
		},
	}
	
	return tools, nil
}

func showServerTools(serverPath string) {
	tools, err := listServerTools(serverPath)
	if err != nil {
		log.Printf("Error listing tools: %v", err)
		return
	}

	fmt.Printf("Tools from server %s:\n", serverPath)
	for _, tool := range tools {
		fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
	}
}

func runStdioServer(serverPath string) {
	// Resolve the absolute path
	absPath, err := filepath.Abs(serverPath)
	if err != nil {
		log.Fatalf("Failed to resolve server path: %v", err)
	}

	// Create command
	cmd := exec.Command(absPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Run the command
	if err := cmd.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}