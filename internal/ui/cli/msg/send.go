package msg

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"github.com/isaacphi/slop/internal/agent"
	"github.com/isaacphi/slop/internal/app"
	"github.com/isaacphi/slop/internal/mcp"
	"github.com/isaacphi/slop/internal/message"
	"github.com/spf13/cobra"
)

var (
	continueFlag    bool
	followupFlag    bool
	modelFlag       string
	threadFlag      string
	noStreamFlag    bool
	maxTokensFlag   int
	temperatureFlag float64
)

var sendCmd = &cobra.Command{
	Use:   "send [message]",
	Short: "Send messages to an LLM",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create cancellable context
		// TODO: do I need this, or should I just use cmd context?
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		// Initialize services
		overrides := &message.MessageServiceOverrides{}
		if modelFlag != "" {
			overrides.ActiveModel = &modelFlag
		}
		if maxTokensFlag > 0 {
			overrides.MaxTokens = &maxTokensFlag
		}
		if temperatureFlag > 0 {
			overrides.Temperature = &temperatureFlag
		}
		cfg := app.Get().Config

		// Initialize services
		service, err := message.InitializeMessageService(cfg, overrides)
		if err != nil {
			return err
		}
		mcpClient := mcp.New(cfg.MCPServers)
		if err := mcpClient.Initialize(context.Background()); err != nil {
			return fmt.Errorf("failed to initialize MCP client: %w", err)
		}
		defer mcpClient.Shutdown()
		agentService := agent.New(service, mcpClient, cfg.Agent)

		// Get the initialMessage content
		var initialMessage string
		if len(args) > 0 {
			initialMessage = strings.Join(args, " ")
		} else {
			// Check for piped input
			stat, _ := os.Stdin.Stat()
			if (stat.Mode() & os.ModeCharDevice) == 0 {
				bytes, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("failed to read piped input: %w", err)
				}
				initialMessage = strings.TrimSpace(string(bytes))
			}
		}

		if initialMessage == "" {
			return fmt.Errorf("no message provided")
		}

		// Get thread ID
		var threadID uuid.UUID
		if continueFlag && threadFlag != "" {
			return fmt.Errorf("cannot specify --target and --continue")
		}
		if threadFlag != "" {
			thread, err := service.FindThreadByPartialID(ctx, threadFlag)
			if err != nil {
				return err
			}
			threadID = thread.ID
		} else if continueFlag {
			thread, err := service.GetActiveThread(ctx)
			if err != nil {
				return err
			}
			threadID = thread.ID
		} else {
			// Create new thread
			thread, err := service.NewThread(ctx)
			if err != nil {
				return fmt.Errorf("failed to create thread: %w", err)
			}
			threadID = thread.ID
		}

		sendOptions := message.SendMessageOptions{
			ThreadID: threadID,
			Content:  initialMessage,
		}

		// Send initial message
		if err := sendMessage(ctx, agentService, sendOptions); err != nil {
			return err
		}

		// Handle followup mode
		if followupFlag {
			reader := bufio.NewReader(os.Stdin)
			for {
				followupMessage, err := reader.ReadString('\n')
				if err == io.EOF {
					break
				}
				if err != nil {
					return fmt.Errorf("failed to read input: %w", err)
				}

				followupMessage = strings.TrimSpace(followupMessage)
				if followupMessage == "" {
					continue
				}

				sendOptions.Content = followupMessage
				if err := sendMessage(ctx, agentService, sendOptions); err != nil {
					return err
				}
			}
		}

		return nil
	},
}

func sendMessage(ctx context.Context, agentService *agent.Agent, opts message.SendMessageOptions) error {
	if !noStreamFlag {
		opts.StreamHandler = &CLIStreamHandler{originalCallback: func(chunk []byte) error {
			fmt.Print(string(chunk))
			return nil
		}}
	}

	errCh := make(chan error, 1)
	go func() {
		resp, err := agentService.SendMessage(ctx, opts)
		if err != nil {
			errCh <- err
			return
		}
		if noStreamFlag {
			fmt.Print(resp.Content)
		}
		// note: gemini does not stream tool use (is this an issue with langchaingo?)
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		fmt.Println("\nRequest cancelled")
		return ctx.Err()
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}
	}

	fmt.Println()
	return nil
}

// Handles function call detection and formatting
type CLIStreamHandler struct {
	originalCallback func([]byte) error
	inQuote          bool
	escaped          bool
	indentLevel      int
	inArray          bool
	isValue          bool // Tracks if we're after a colon to handle YAML formatting
}

func (h *CLIStreamHandler) HandleTextChunk(chunk []byte) error {
	return h.originalCallback(chunk)
}

func (h *CLIStreamHandler) HandleMessageDone() error {
	fmt.Print("\n\n")
	return nil
}

func (h *CLIStreamHandler) HandleFunctionCallStart(id, name string) error {
	fmt.Printf("\n\n[Requesting tool use: %s]", name)
	return nil
}

func (h *CLIStreamHandler) HandleFunctionCallChunk(chunk message.FunctionCallChunk) error {
	fmt.Print(h.formatJSON(chunk.ArgumentsJson))
	return nil
}

func (h *CLIStreamHandler) Reset() {
	h.inQuote = false
	h.escaped = false
}

func (h *CLIStreamHandler) formatJSON(data string) string {
	var formatted strings.Builder

	writeIndent := func() {
		for i := 0; i < h.indentLevel; i++ {
			formatted.WriteString("  ")
		}
	}

	for _, char := range data {
		switch {
		case h.escaped:
			formatted.WriteRune(char)
			h.escaped = false

		case char == '\\':
			formatted.WriteRune(char)
			h.escaped = true

		case char == '"':
			// For YAML, we generally don't need the quotes unless there's special characters
			if !h.inQuote {
				// Starting a quote - don't write it
				h.inQuote = true
			} else {
				// Ending a quote - don't write it
				h.inQuote = false
			}

		case char == '[' && !h.inQuote:
			h.inArray = true
			h.indentLevel++
			formatted.WriteString("\n")
			writeIndent()
			formatted.WriteString("- ")

		case char == ']' && !h.inQuote:
			h.indentLevel--
			h.inArray = false

		case char == '{' && !h.inQuote:
			if h.inArray {
				writeIndent()
			}
			h.indentLevel++
			formatted.WriteString("\n")
			writeIndent()

		case char == '}' && !h.inQuote:
			h.indentLevel--
			h.isValue = false

		case char == ',' && !h.inQuote:
			h.isValue = false
			formatted.WriteString("\n")
			if h.inArray {
				writeIndent()
				formatted.WriteString("- ")
			} else {
				writeIndent()
			}

		case char == ':' && !h.inQuote:
			h.isValue = true
			formatted.WriteString(": ")

		case char == ' ' && !h.inQuote:
			// Only keep spaces that are part of actual values
			if h.isValue {
				formatted.WriteRune(char)
			}

		default:
			if h.inArray && !h.isValue && !h.inQuote {
				// If we're starting a new array element
				formatted.WriteString("- ")
				h.isValue = true
			}
			formatted.WriteRune(char)
		}
	}
	return formatted.String()
}

func init() {
	sendCmd.Flags().StringVarP(&threadFlag, "thread", "t", "", "Continue target thread")
	sendCmd.Flags().BoolVarP(&continueFlag, "continue", "c", false, "Continue the most recent thread")
	sendCmd.Flags().BoolVarP(&followupFlag, "followup", "f", false, "Enable followup mode")
	sendCmd.Flags().StringVarP(&modelFlag, "model", "m", "", "Specify the model to use")
	sendCmd.Flags().BoolVarP(&noStreamFlag, "no-stream", "n", false, "Disable streaming of responses")
	sendCmd.Flags().IntVar(&maxTokensFlag, "max-tokens", 0, "Override maximum length")
	sendCmd.Flags().Float64Var(&temperatureFlag, "temperature", 0, "Override temperature")
}
