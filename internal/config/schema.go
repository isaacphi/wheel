package config

// LLM presets
type Model struct {
	Provider    string          `mapstructure:"provider"`
	Name        string          `mapstructure:"name"`
	MaxTokens   int             `mapstructure:"MaxTokens"`
	Temperature float64         `mapstructure:"temperature"`
	Tools       map[string]Tool `mapstructure:"tools"`
}

type Tool struct {
	Name        string     `mapstructure:"name"`
	Description string     `mapstructure:"description"`
	Parameters  Parameters `mapstructure:"parameters"`
}

type Parameters struct {
	Type       string              `mapstructure:"type"`
	Properties map[string]Property `mapstructure:"properties"`
	Required   []string            `mapstructure:"required"`
}

type Property struct {
	Type        string              `mapstructure:"type"`
	Description string              `mapstructure:"description"`
	Enum        []string            `mapstructure:"enum"`
	Items       *Property           `mapstructure:"items"`      // For array types
	Properties  map[string]Property `mapstructure:"properties"` // For object types
	Required    []string            `mapstructure:"required"`   // For object types
	Default     interface{}         `mapstructure:"default"`    // For properties with default values
}

// Internal configuration settings
type Internal struct {
	Model         string `mapstructure:"model"`
	SummaryPrompt string `mapstructure:"summaryPrompt"`
}

// MCP
type MCPServer struct {
	Command string            `mapstructure:"command"`
	Args    []string          `mapstructure:"args"`
	Env     map[string]string `mapstructure:"env"`
}

// "Agent"
type Agent struct {
	AutoApproveFunctions bool `mapstructure:"autoApproveFunctions"`
}

// Logs
type Log struct {
	LogLevel string `mapstructure:"logLevel"`
	LogFile  string `mapstructure:"logFile"`
}

type ConfigSchema struct {
	Models      map[string]Model     `mapstructure:"models"`
	ActiveModel string               `mapstructure:"activeModel"`
	DBPath      string               `mapstructure:"dbPath"`
	Internal    Internal             `mapstructure:"internal"`
	MCPServers  map[string]MCPServer `mapstructure:"mcpServers"`
	Agent       Agent                `mapstructure:"agent"`
	Log         Log                  `mapstructure:"log"`

	// Internal fields for printing
	sources  map[string]string
	warnings []string
}
