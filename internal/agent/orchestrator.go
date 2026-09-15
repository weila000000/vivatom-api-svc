package agent

import (
	"context"

	"vivatom-api-svc/internal/ai"
	"vivatom-api-svc/internal/domain"
)

type Orchestrator struct {
	provider ai.Provider
	guard    SnapshotGuard
}

type emitter func(domain.AgentEvent) bool

type SnapshotGuard interface {
	Check(domain.ProjectSnapshot) (domain.ProjectSnapshot, error)
}

func NewOrchestrator(provider ai.Provider, guard SnapshotGuard) *Orchestrator {
	return &Orchestrator{provider: provider, guard: guard}
}

func (o *Orchestrator) Run(ctx context.Context, request domain.AgentRequest) (<-chan domain.AgentEvent, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	events := make(chan domain.AgentEvent)
	go func() {
		defer close(events)
		send := func(event domain.AgentEvent) bool {
			select {
			case events <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}
		if request.Action == domain.ActionPlan {
			o.runPlan(ctx, request, send)
		} else if request.Action == domain.ActionBuild {
			o.runBuild(ctx, request, send)
		} else {
			o.runRevision(ctx, request, send)
		}
	}()
	return events, nil
}

func (o *Orchestrator) runRevision(ctx context.Context, request domain.AgentRequest, send emitter) {
	labels := map[domain.AgentAction]string{domain.ActionIterate: "正在实现迭代需求", domain.ActionRepair: "正在修复当前版本", domain.ActionPolish: "正在优化产品体验"}
	if !send(domain.AgentEvent{Type: "agent.started", Agent: "bob", Message: labels[request.Action]}) || !send(domain.AgentEvent{Type: "action.status", ID: "revise", Agent: "bob", Action: string(request.Action), Status: "running", Label: labels[request.Action]}) {
		return
	}
	instruction := request.Prompt
	if request.Action == domain.ActionRepair {
		instruction = request.Error
	}
	snapshot, err := o.provider.Revise(ctx, request.Action, instruction, *request.Snapshot)
	if err != nil {
		o.sendProviderFailure(ctx, send, err)
		return
	}
	if err = domain.ValidateRevisionContract(*request.Snapshot, snapshot); err != nil {
		o.sendFailure(ctx, send, "contract_rejected", "候选源码偏离当前版本的数据契约。")
		return
	}
	snapshot, err = o.guard.Check(snapshot)
	if err != nil {
		o.sendFailure(ctx, send, "snapshot_rejected", "候选源码未通过安全检查。")
		return
	}
	if !send(domain.AgentEvent{Type: "action.status", ID: "revise", Agent: "bob", Action: string(request.Action), Status: "completed", Label: "新候选快照已生成"}) || !send(domain.AgentEvent{Type: "snapshot.completed", Snapshot: &snapshot}) {
		return
	}
	send(domain.AgentEvent{Type: "done"})
}

func (o *Orchestrator) runPlan(ctx context.Context, request domain.AgentRequest, send emitter) {
	if !send(domain.AgentEvent{
		Type: "agent.started", Agent: "mike", Message: "正在理解你的产品需求",
	}) || !send(domain.AgentEvent{
		Type: "action.status", ID: "scope", Agent: "mike",
		Action: "scope_requirements", Status: "running", Label: "梳理需求",
	}) || !send(domain.AgentEvent{
		Type: "agent.output", ID: "scope-output", Agent: "mike",
		Text: "正在识别目标用户、核心场景和验收条件。",
	}) {
		return
	}

	plan, err := o.provider.Plan(ctx, request.Prompt)
	if err != nil {
		o.sendProviderFailure(ctx, send, err)
		return
	}
	if !send(domain.AgentEvent{
		Type: "action.status", ID: "scope", Agent: "mike",
		Action: "scope_requirements", Status: "completed", Label: "需求已梳理",
	}) || !send(domain.AgentEvent{
		Type: "agent.completed", Agent: "mike",
		Message: "产品方案已经准备好，请确认后再开始构建。",
	}) || !send(domain.AgentEvent{Type: "approval.required", Plan: &plan}) {
		return
	}
	send(domain.AgentEvent{Type: "done"})
}

func (o *Orchestrator) runBuild(ctx context.Context, request domain.AgentRequest, send emitter) {
	if !send(domain.AgentEvent{
		Type: "agent.started", Agent: "bob", Message: "正在根据已批准方案生成源码",
	}) || !send(domain.AgentEvent{
		Type: "action.status", ID: "generate", Agent: "bob",
		Action: "generate_artifact", Status: "running", Label: "生成完整源码快照",
	}) || !send(domain.AgentEvent{
		Type: "agent.output", ID: "build-output", Agent: "bob",
		Text: "正在创建入口、页面组件和基础样式。",
	}) {
		return
	}

	snapshot, err := o.provider.Build(ctx, request.Prompt, *request.Plan)
	if err != nil {
		o.sendProviderFailure(ctx, send, err)
		return
	}
	if err = domain.ValidateBuildContract(*request.Plan, snapshot); err != nil {
		o.sendFailure(ctx, send, "contract_rejected", "候选源码没有完整实现已批准方案。")
		return
	}
	snapshot, err = o.guard.Check(snapshot)
	if err != nil {
		o.sendFailure(ctx, send, "snapshot_rejected", "候选源码未通过安全检查。")
		return
	}
	if !send(domain.AgentEvent{
		Type: "action.status", ID: "generate", Agent: "bob",
		Action: "generate_artifact", Status: "completed", Label: "候选源码已生成",
	}) || !send(domain.AgentEvent{
		Type: "agent.completed", Agent: "bob",
		Message: "候选源码已经生成，等待安全检查和编译。",
	}) || !send(domain.AgentEvent{Type: "snapshot.completed", Snapshot: &snapshot}) {
		return
	}
	send(domain.AgentEvent{Type: "done"})
}

func (o *Orchestrator) sendFailure(ctx context.Context, send emitter, code string, message string) {
	if ctx.Err() != nil {
		return
	}
	if send(domain.AgentEvent{
		Type: "error", Code: code, Message: message, Retryable: true,
	}) {
		send(domain.AgentEvent{Type: "done"})
	}
}

func (o *Orchestrator) sendProviderFailure(ctx context.Context, send emitter, err error) {
	code, message, retryable := ai.NormalizeError(err)
	if ctx.Err() != nil {
		return
	}
	if send(domain.AgentEvent{Type: "error", Code: code, Message: message, Retryable: retryable}) {
		send(domain.AgentEvent{Type: "done"})
	}
}
