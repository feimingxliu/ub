// Package agent implements ub's headless provider/tool loop.
package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/pkg/core/message"
	contextmgr "github.com/feimingxliu/ub/internal/pkg/llm/context"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
	"github.com/feimingxliu/ub/internal/pkg/runtime/hook"
	"github.com/feimingxliu/ub/internal/pkg/tool"
	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
	"golang.org/x/sync/errgroup"
)

// Run executes one user prompt.
func (a *Agent) Run(ctx context.Context, req Request) (Result, error) {
	if req.Turn <= 0 {
		req.Turn = 1
	}
	defer func() {
		// Wrapped in a closure so the hooks.Run call itself is deferred,
		// not just emitHookOutcomes — otherwise the Run would fire before
		// the loop even starts (defer args are evaluated eagerly).
		a.emitHookOutcomes(a.hooks.Run(ctx, hook.Event{
			Kind:      hook.KindPostUserTurn,
			SessionID: req.SessionID,
			Turn:      req.Turn,
		}))
	}()
	a.emitHookOutcomes(a.hooks.Run(ctx, hook.Event{
		Kind:      hook.KindPreUserTurn,
		SessionID: req.SessionID,
		Turn:      req.Turn,
	}))

	userMsg := message.Text(message.RoleUser, req.Prompt)
	transcriptMessages := cloneMessages(req.History)
	transcriptMessages = append(transcriptMessages, userMsg)
	contextMessages := cloneMessages(req.ContextHistory)
	if len(contextMessages) == 0 {
		contextMessages = cloneMessages(req.History)
	}
	contextMessages = append(contextMessages, userMsg)
	if a.fileHistory != nil && !a.fileHistoryToolsOnly {
		if err := a.fileHistory.MakeSnapshot(ctx, req.Turn); err != nil {
			a.emit(Event{Type: EventError, Content: fmt.Sprintf("file history snapshot: %v", err), IsError: true, Err: err})
		}
	}
	if err := a.append(ctx, req.SessionID, func() (rollout.Event, error) {
		return rollout.UserMessage(req.SessionID, req.Turn, userMsg)
	}); err != nil {
		return Result{}, err
	}

	turn := 0
	limit := a.maxTurns
	outputTokensRecoveryCount := 0
	contextOverflowRecoveryUsed := false
	loopDetector := newToolLoopDetector(repeatedToolWindowSize, repeatedToolMaxRepeats)
loop:
	for {
		for limit <= 0 || turn < limit {
			mode := a.currentMode()
			tools, err := a.toolDefinitions(mode)
			if err != nil {
				return Result{}, err
			}
			prepared, err := a.prepareMessages(ctx, req.SessionID, req.Turn, contextMessages, tools)
			if err != nil {
				return Result{}, a.recordError(ctx, req.SessionID, req.Turn, err)
			}
			contextMessages = prepared.messages
			stream, err := a.provider.Chat(ctx, provider.Request{
				Model:     a.model,
				Messages:  cloneMessages(prepared.requestMessages),
				Tools:     tools,
				Reasoning: cloneReasoning(a.reasoning),
			})
			if err != nil {
				recovered, recoveryErr := a.recoverContextOverflow(ctx, req.SessionID, req.Turn, contextMessages, prepared.estimatedTokens, tools, err, contextOverflowRecoveryUsed)
				if recoveryErr != nil {
					return Result{}, a.recordError(ctx, req.SessionID, req.Turn, recoveryErr)
				}
				if recovered.recovered {
					contextOverflowRecoveryUsed = true
					contextMessages = recovered.messages
					continue
				}
				return Result{}, a.recordError(ctx, req.SessionID, req.Turn, err)
			}
			consumed, err := a.consumeStream(ctx, req.SessionID, req.Turn, stream, prepared.estimatedTokens)
			closeErr := stream.Close()
			if err != nil {
				recovered, recoveryErr := a.recoverContextOverflow(ctx, req.SessionID, req.Turn, contextMessages, prepared.estimatedTokens, tools, err, contextOverflowRecoveryUsed)
				if recoveryErr != nil {
					return Result{}, a.recordError(ctx, req.SessionID, req.Turn, recoveryErr)
				}
				if recovered.recovered {
					contextOverflowRecoveryUsed = true
					contextMessages = recovered.messages
					continue
				}
				return Result{}, a.recordError(ctx, req.SessionID, req.Turn, err)
			}
			if closeErr != nil {
				recovered, recoveryErr := a.recoverContextOverflow(ctx, req.SessionID, req.Turn, contextMessages, prepared.estimatedTokens, tools, closeErr, contextOverflowRecoveryUsed)
				if recoveryErr != nil {
					return Result{}, a.recordError(ctx, req.SessionID, req.Turn, recoveryErr)
				}
				if recovered.recovered {
					contextOverflowRecoveryUsed = true
					contextMessages = recovered.messages
					continue
				}
				return Result{}, a.recordError(ctx, req.SessionID, req.Turn, closeErr)
			}
			if len(consumed.toolCalls) == 0 && len(consumed.message.Content) == 0 && consumed.reasoningLen > 0 && outputTokensRecoveryCount < maxOutputTokensRecoveryLimit {
				outputTokensRecoveryCount++
				a.emit(Event{
					Type:         EventActivity,
					ActivityKind: ActivityNotice,
					Status:       "running",
					Summary:      fmt.Sprintf("output token limit hit during reasoning; recovery attempt %d/%d", outputTokensRecoveryCount, maxOutputTokensRecoveryLimit),
				})
				recoveryMsg := message.Text(message.RoleUser, outputTokensRecoveryInstruction)
				contextMessages = append(contextMessages, recoveryMsg)
				continue
			}
			if len(consumed.message.Content) > 0 {
				transcriptMessages = append(transcriptMessages, consumed.message)
				contextMessages = append(contextMessages, consumed.message)
				if err := a.append(ctx, req.SessionID, func() (rollout.Event, error) {
					return rollout.AssistantMessage(req.SessionID, req.Turn, consumed.message)
				}); err != nil {
					return Result{}, err
				}
			}
			if len(consumed.toolCalls) == 0 {
				if len(consumed.message.Content) == 0 {
					return Result{}, a.recordError(ctx, req.SessionID, req.Turn, emptyResponseError(consumed.reasoningLen))
				}
				a.finishSuccessfulTurn(ctx, req.SessionID, req.Turn, transcriptMessages, consumed.text)
				return Result{Text: consumed.text, Messages: transcriptMessages, ContextMessages: contextMessages}, nil
			}
			// Execute tool calls concurrently, then collect results in order.
			type indexedResult struct {
				call   toolCall
				result tool.Result
			}
			results := make([]indexedResult, len(consumed.toolCalls))
			g, gctx := errgroup.WithContext(ctx)
			g.SetLimit(len(consumed.toolCalls))
			for i, call := range consumed.toolCalls {
				i, call := i, call
				g.Go(func() error {
					r := a.runTool(gctx, req.SessionID, req.Turn, call)
					results[i] = indexedResult{call: call, result: r}
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				return Result{}, err
			}
			toolResults := make([]tool.Result, 0, len(results))
			for _, ir := range results {
				toolResults = append(toolResults, ir.result)
				toolResultMessage := message.New(message.RoleTool, message.ToolResultBlock(ir.call.ID, ir.result.Content, ir.result.IsError))
				transcriptMessages = append(transcriptMessages, toolResultMessage)
				contextMessages = append(contextMessages, toolResultMessage)
				if err := a.append(ctx, req.SessionID, func() (rollout.Event, error) {
					return rollout.ToolResult(req.SessionID, req.Turn, ir.call.ID, ir.call.Name, ir.result)
				}); err != nil {
					return Result{}, err
				}
			}
			turn++
			if loopDetector.Record(consumed.toolCalls, toolResults) {
				return a.finalizeWithoutTools(ctx, req.SessionID, req.Turn, contextMessages, transcriptMessages, "repeated tool loop detected; finalizing without tools")
			}
		}
		// Hit the limit. Ask the host whether to keep going before falling
		// through to finalizeWithoutTools — reasoning models often have more
		// tool calls queued up and the no-tool path produces bad output for
		// them (e.g. native tool-call syntax leaking into the text).
		if a.limitAsker != nil {
			resp, err := a.limitAsker.AskExtension(ctx, LimitExtensionRequest{
				SessionID: req.SessionID,
				UserTurn:  req.Turn,
				UsedTurns: turn,
			})
			if err != nil {
				return Result{}, a.recordError(ctx, req.SessionID, req.Turn, err)
			}
			if resp.ExtraTurns > 0 {
				limit += resp.ExtraTurns
				continue loop
			}
		}
		break
	}
	return a.finalizeWithoutTools(ctx, req.SessionID, req.Turn, contextMessages, transcriptMessages, fmt.Sprintf("tool loop reached %d turns; finalizing without tools", limit))
}

func (a *Agent) finalizeWithoutTools(ctx context.Context, sessionID string, turn int, contextMessages, transcriptMessages []message.Message, summary string) (Result, error) {
	if strings.TrimSpace(summary) == "" {
		summary = "tool loop finalizing without tools"
	}
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityNotice,
		Status:       "running",
		Summary:      summary,
	})

	requestMessages := cloneMessages(contextMessages)
	requestMessages = append(requestMessages, message.Text(message.RoleSystem, maxTurnsFinalInstruction))
	providerMessages := a.withRuntimeContext(requestMessages)
	estimated := contextmgr.EstimateRequest(providerMessages, nil, a.model)
	a.emitContextUsage(estimated, false)
	// Reasoning is deliberately omitted from this final no-tool request.
	// Reasoning models (e.g. OpenAI o-series) frequently return an empty stream
	// when reasoning is configured but the prompt forbids tool calls,
	// surfacing to the user as `max turns reached: final no-tool response was empty`.
	// Without Reasoning the recovery path produces a normal text response.
	stream, err := a.provider.Chat(ctx, provider.Request{
		Model:    a.model,
		Messages: cloneMessages(providerMessages),
	})
	if err != nil {
		return Result{}, a.recordError(ctx, sessionID, turn, fmt.Errorf("%w: final no-tool request failed: %v", ErrMaxTurns, err))
	}
	consumed, err := a.consumeStream(ctx, sessionID, turn, stream, estimated)
	closeErr := stream.Close()
	if err != nil {
		return Result{}, a.recordError(ctx, sessionID, turn, fmt.Errorf("%w: final no-tool stream failed: %v", ErrMaxTurns, err))
	}
	if closeErr != nil {
		return Result{}, a.recordError(ctx, sessionID, turn, fmt.Errorf("%w: final no-tool stream close failed: %v", ErrMaxTurns, closeErr))
	}
	if len(consumed.toolCalls) > 0 {
		return Result{}, a.recordError(ctx, sessionID, turn, fmt.Errorf("%w: final no-tool response still requested %d tool call(s)", ErrMaxTurns, len(consumed.toolCalls)))
	}
	if len(consumed.message.Content) == 0 {
		return Result{}, a.recordError(ctx, sessionID, turn, fmt.Errorf("%w: final no-tool response was empty", ErrMaxTurns))
	}
	transcriptMessages = append(transcriptMessages, consumed.message)
	contextMessages = append(contextMessages, consumed.message)
	if err := a.append(ctx, sessionID, func() (rollout.Event, error) {
		return rollout.AssistantMessage(sessionID, turn, consumed.message)
	}); err != nil {
		return Result{}, err
	}
	a.finishSuccessfulTurn(ctx, sessionID, turn, transcriptMessages, consumed.text)
	return Result{Text: consumed.text, Messages: transcriptMessages, ContextMessages: contextMessages}, nil
}
