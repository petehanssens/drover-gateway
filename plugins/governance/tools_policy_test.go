package governance

import (
	"context"
	"testing"
	"time"

	"github.com/petehanssens/drover-gateway/core/schemas"
	"github.com/petehanssens/drover-gateway/framework/configstore"
	configstoreTables "github.com/petehanssens/drover-gateway/framework/configstore/tables"
	"github.com/stretchr/testify/require"
)

func TestPreLLMHook_StripsToolsWhenVirtualKeyDisablesTools(t *testing.T) {
	logger := NewMockLogger()

	toolsEnabled := false
	vk := buildVirtualKeyWithProviders("vk1", "sk-bf-test", "drover-internal-key", []configstoreTables.TableVirtualKeyProviderConfig{
		buildProviderConfig("openai", []string{"*"}),
	})
	vk.ToolsEnabled = &toolsEnabled

	isMandatory := true
	plugin, err := Init(context.Background(), &Config{IsVkMandatory: &isMandatory}, logger, nil, &configstore.GovernanceConfig{
		VirtualKeys: []configstoreTables.TableVirtualKey{*vk},
	}, nil, nil, nil)
	require.NoError(t, err)

	t.Run("chat", func(t *testing.T) {
		ctx := schemas.NewBifrostContext(context.Background(), time.Time{})
		ctx.SetValue(schemas.BifrostContextKeyVirtualKey, "sk-bf-test")

		auto := string(schemas.ChatToolChoiceTypeAuto)
		req := &schemas.BifrostRequest{
			RequestType: schemas.ChatCompletionRequest,
			ChatRequest: &schemas.BifrostChatRequest{
				Provider: schemas.OpenAI,
				Model:    "gpt-4o-mini",
				Params: &schemas.ChatParameters{
					Tools: []schemas.ChatTool{
						{
							Type: schemas.ChatToolTypeFunction,
							Function: &schemas.ChatToolFunction{
								Name: "demo_tool",
							},
						},
					},
					ToolChoice: &schemas.ChatToolChoice{ChatToolChoiceStr: &auto},
					MCPServers: []schemas.ChatMCPServer{{Name: "mcp-demo"}},
				},
			},
		}

		out, shortCircuit, hookErr := plugin.PreLLMHook(ctx, req)
		require.NoError(t, hookErr)
		require.Nil(t, shortCircuit)
		require.NotNil(t, out)
		require.NotNil(t, out.ChatRequest)
		require.NotNil(t, out.ChatRequest.Params)
		require.Len(t, out.ChatRequest.Params.Tools, 0)
		require.Nil(t, out.ChatRequest.Params.ToolChoice)
		require.Len(t, out.ChatRequest.Params.MCPServers, 0)
	})

	t.Run("responses", func(t *testing.T) {
		ctx := schemas.NewBifrostContext(context.Background(), time.Time{})
		ctx.SetValue(schemas.BifrostContextKeyVirtualKey, "sk-bf-test")

		req := &schemas.BifrostRequest{
			RequestType: schemas.ResponsesRequest,
			ResponsesRequest: &schemas.BifrostResponsesRequest{
				Provider: schemas.OpenAI,
				Model:    "gpt-4o-mini",
				Params: &schemas.ResponsesParameters{
					Tools: []schemas.ResponsesTool{
						{
							Type:                  schemas.ResponsesToolTypeFunction,
							Name:                  schemas.Ptr("demo_tool"),
							ResponsesToolFunction: &schemas.ResponsesToolFunction{Strict: schemas.Ptr(true)},
						},
					},
					ToolChoice: &schemas.ResponsesToolChoice{ResponsesToolChoiceStr: schemas.Ptr(string(schemas.ResponsesToolChoiceTypeAuto))},
				},
			},
		}

		out, shortCircuit, hookErr := plugin.PreLLMHook(ctx, req)
		require.NoError(t, hookErr)
		require.Nil(t, shortCircuit)
		require.NotNil(t, out)
		require.NotNil(t, out.ResponsesRequest)
		require.NotNil(t, out.ResponsesRequest.Params)
		require.Len(t, out.ResponsesRequest.Params.Tools, 0)
		require.Nil(t, out.ResponsesRequest.Params.ToolChoice)
	})
}

func TestPreLLMHook_XSourceDroverBrainStripsToolsEvenWhenVirtualKeyAllowsTools(t *testing.T) {
	logger := NewMockLogger()
	vk := buildVirtualKeyWithProviders("vk1", "sk-bf-test", "drover-agent-key", []configstoreTables.TableVirtualKeyProviderConfig{
		buildProviderConfig("openai", []string{"*"}),
	})
	// Default: tools enabled (nil ToolsEnabled)

	isMandatory := true
	plugin, err := Init(context.Background(), &Config{IsVkMandatory: &isMandatory}, logger, nil, &configstore.GovernanceConfig{
		VirtualKeys: []configstoreTables.TableVirtualKey{*vk},
	}, nil, nil, nil)
	require.NoError(t, err)

	ctx := schemas.NewBifrostContext(context.Background(), time.Time{})
	ctx.SetValue(schemas.BifrostContextKeyVirtualKey, "sk-bf-test")
	ctx.SetValue(schemas.BifrostContextKeyXSource, "drover-brain")

	auto := string(schemas.ChatToolChoiceTypeAuto)
	req := &schemas.BifrostRequest{
		RequestType: schemas.ChatCompletionRequest,
		ChatRequest: &schemas.BifrostChatRequest{
			Provider: schemas.OpenAI,
			Model:    "gpt-4o-mini",
			Params: &schemas.ChatParameters{
				Tools: []schemas.ChatTool{
					{Type: schemas.ChatToolTypeFunction, Function: &schemas.ChatToolFunction{Name: "demo_tool"}},
				},
				ToolChoice: &schemas.ChatToolChoice{ChatToolChoiceStr: &auto},
			},
		},
	}

	out, shortCircuit, hookErr := plugin.PreLLMHook(ctx, req)
	require.NoError(t, hookErr)
	require.Nil(t, shortCircuit)
	require.Len(t, out.ChatRequest.Params.Tools, 0)
	require.Nil(t, out.ChatRequest.Params.ToolChoice)
}

func TestPreLLMHook_WithoutXSourcePreservesToolsWhenVirtualKeyAllowsTools(t *testing.T) {
	logger := NewMockLogger()
	vk := buildVirtualKeyWithProviders("vk1", "sk-bf-test", "drover-agent-key", []configstoreTables.TableVirtualKeyProviderConfig{
		buildProviderConfig("openai", []string{"*"}),
	})

	isMandatory := true
	plugin, err := Init(context.Background(), &Config{IsVkMandatory: &isMandatory}, logger, nil, &configstore.GovernanceConfig{
		VirtualKeys: []configstoreTables.TableVirtualKey{*vk},
	}, nil, nil, nil)
	require.NoError(t, err)

	ctx := schemas.NewBifrostContext(context.Background(), time.Time{})
	ctx.SetValue(schemas.BifrostContextKeyVirtualKey, "sk-bf-test")

	auto := string(schemas.ChatToolChoiceTypeAuto)
	req := &schemas.BifrostRequest{
		RequestType: schemas.ChatCompletionRequest,
		ChatRequest: &schemas.BifrostChatRequest{
			Provider: schemas.OpenAI,
			Model:    "gpt-4o-mini",
			Params: &schemas.ChatParameters{
				Tools: []schemas.ChatTool{
					{Type: schemas.ChatToolTypeFunction, Function: &schemas.ChatToolFunction{Name: "demo_tool"}},
				},
				ToolChoice: &schemas.ChatToolChoice{ChatToolChoiceStr: &auto},
			},
		},
	}

	out, shortCircuit, hookErr := plugin.PreLLMHook(ctx, req)
	require.NoError(t, hookErr)
	require.Nil(t, shortCircuit)
	require.Len(t, out.ChatRequest.Params.Tools, 1)
	require.NotNil(t, out.ChatRequest.Params.ToolChoice)
}
