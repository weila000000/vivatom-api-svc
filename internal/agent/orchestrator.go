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
		} else if request.Action == domain.ActionRace {
			o.runRace(ctx, request, send)
		} else {
			o.runRevision(ctx, request, send)
		}
	}()
	return events, nil
}

func (o *Orchestrator) runRevision(ctx context.Context, request domain.AgentRequest, send emitter) {
	labels := map[domain.AgentAction]string{domain.ActionIterate: "正在实现迭代需求", domain.ActionRepair: "正在修复当前版本", domain.ActionPolish: "正在优化产品体验"}
	if !send(domain.AgentEvent{Type: "agent.started", Agent: "alex", Message: labels[request.Action]}) || !send(domain.AgentEvent{Type: "action.status", ID: "revise", Agent: "alex", Action: string(request.Action), Status: "running", Label: labels[request.Action]}) {
		return
	}
	instruction := request.Prompt
	if request.Action == domain.ActionRepair {
		instruction = request.Error
	}
	snapshot, err := o.provider.Revise(ctx, request.Action, instruction, *request.Snapshot)
	if err != nil {
		if !send(domain.AgentEvent{Type: "warning", Code: "local_fallback", Message: "模型修订失败，已切换本地模板通道。"}) {
			return
		}
		snapshot, err = (&ai.FakeProvider{}).Revise(ctx, request.Action, instruction, *request.Snapshot)
		if err != nil {
			o.sendProviderFailure(ctx, send, err)
			return
		}
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
	if !send(domain.AgentEvent{Type: "action.status", ID: "revise", Agent: "alex", Action: string(request.Action), Status: "completed", Label: "新版本已生成"}) || !send(domain.AgentEvent{Type: "snapshot.completed", Snapshot: &snapshot}) {
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

	mode := request.Mode
	if mode == "" {
		mode = domain.ModeTeam
	}
	var plan domain.BuildPlan
	var err error
	if mode == domain.ModeEngineer {
		if !send(domain.AgentEvent{Type: "action.status", ID: "scope", Agent: "mike", Action: "scope_requirements", Status: "completed", Label: "需求已接收"}) || !send(domain.AgentEvent{Type: "agent.completed", Agent: "mike", Message: "任务已交给 Alex。"}) || !send(domain.AgentEvent{Type: "agent.started", Agent: "alex", Message: "正在制定轻量工程计划"}) {
			return
		}
		plan, err = o.provider.Plan(ctx, request.Prompt)
	} else if collaborative, ok := o.provider.(ai.CollaborativePlanner); ok {
		var brief domain.RequirementBrief
		brief, err = collaborative.AnalyzeRequirements(ctx, request.Prompt)
		if err == nil {
			if !send(domain.AgentEvent{Type: "action.status", ID: "scope", Agent: "mike", Action: "scope_requirements", Status: "completed", Label: "需求简报已生成"}) ||
				!send(domain.AgentEvent{Type: "agent.completed", Agent: "mike", Message: brief.Goal}) ||
				!send(domain.AgentEvent{Type: "agent.started", Agent: "emma", Message: "产品目标、用户和核心功能已经明确"}) ||
				!send(domain.AgentEvent{Type: "agent.completed", Agent: "emma", Message: brief.Goal}) ||
				!send(domain.AgentEvent{Type: "agent.started", Agent: "bob", Message: "正在根据产品计划制定技术方案"}) ||
				!send(domain.AgentEvent{Type: "action.status", ID: "plan-contract", Agent: "bob", Action: "plan_implementation", Status: "running", Label: "规划文件、组件和交互"}) {
				return
			}
			plan, err = collaborative.PlanFromBrief(ctx, request.Prompt, brief)
		}
	} else {
		plan, err = o.provider.Plan(ctx, request.Prompt)
		if err == nil && (!send(domain.AgentEvent{Type: "action.status", ID: "scope", Agent: "mike", Action: "scope_requirements", Status: "completed", Label: "需求已梳理"}) ||
			!send(domain.AgentEvent{Type: "agent.completed", Agent: "mike", Message: "产品需求已经结构化。"}) ||
			!send(domain.AgentEvent{Type: "agent.started", Agent: "emma", Message: "正在形成产品计划"}) ||
			!send(domain.AgentEvent{Type: "agent.completed", Agent: "emma", Message: "产品目标和功能已经明确。"}) ||
			!send(domain.AgentEvent{Type: "agent.started", Agent: "bob", Message: "正在形成技术计划"}) ||
			!send(domain.AgentEvent{Type: "action.status", ID: "plan-contract", Agent: "bob", Action: "plan_implementation", Status: "running", Label: "校验页面、文件和交互"})) {
			return
		}
	}
	if err != nil {
		if !send(domain.AgentEvent{Type: "warning", Code: "local_fallback", Message: "模型计划失败，已切换本地模板通道。"}) {
			return
		}
		plan, err = (&ai.FakeProvider{}).Plan(ctx, request.Prompt)
		if err != nil {
			o.sendProviderFailure(ctx, send, err)
			return
		}
	}
	if err = domain.ValidateBuildPlan(plan); err != nil {
		o.sendFailure(ctx, send, "plan_rejected", "生成的方案不符合可执行契约，请重新规划。")
		return
	}
	if !send(domain.AgentEvent{
		Type: "action.status", ID: "plan-contract", Agent: planAgent(mode),
		Action: "plan_implementation", Status: "completed", Label: "计划已通过契约检查",
	}) || !send(domain.AgentEvent{
		Type: "agent.completed", Agent: planAgent(mode),
		Message: "统一计划已经准备好，请批准后开始生成。",
	}) || !send(domain.AgentEvent{Type: "approval.required", Plan: &plan}) {
		return
	}
	send(domain.AgentEvent{Type: "done"})
}

func (o *Orchestrator) runBuild(ctx context.Context, request domain.AgentRequest, send emitter) {
	if !send(domain.AgentEvent{
		Type: "agent.started", Agent: "alex", Message: "正在根据批准计划生成 React 应用",
	}) || !send(domain.AgentEvent{
		Type: "action.status", ID: "build", Agent: "alex", Action: "generate_snapshot", Status: "running", Label: "生成多文件 React 源码",
	}) {
		return
	}

	snapshot, err := o.provider.Build(ctx, request.Prompt, *request.Plan)
	if err != nil {
		if !send(domain.AgentEvent{Type: "warning", Code: "local_fallback", Message: "模型生成失败，已切换本地模板通道。"}) {
			return
		}
		snapshot, err = (&ai.FakeProvider{}).Build(ctx, request.Prompt, *request.Plan)
		if err != nil {
			o.sendProviderFailure(ctx, send, err)
			return
		}
	}
	if !send(domain.AgentEvent{Type: "snapshot.validating", Agent: "alex", Message: "正在校验源码结构、依赖和安全边界"}) {
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
		Type: "action.status", ID: "build", Agent: "alex",
		Action: "generate_snapshot", Status: "completed", Label: "源码契约与安全检查通过",
	}) || !send(domain.AgentEvent{
		Type: "agent.completed", Agent: "alex",
		Message: "React 应用已经生成，可以进入预览。",
	}) || !send(domain.AgentEvent{Type: "snapshot.completed", Snapshot: &snapshot}) {
		return
	}
	send(domain.AgentEvent{Type: "done"})
}

func planAgent(mode domain.WorkMode) string {
	if mode == domain.ModeEngineer {
		return "alex"
	}
	return "bob"
}

func (o *Orchestrator) runRace(ctx context.Context, request domain.AgentRequest, send emitter) {
	directions := []struct{ id, text string }{{"candidate-a", "克制、产品化、信息层级优先"}, {"candidate-b", "表达性更强、视觉冲击和动效优先"}}
	if !send(domain.AgentEvent{Type: "agent.started", Agent: "alex", Message: "正在并行生成两个视觉方案"}) {
		return
	}
	type result struct {
		index    int
		snapshot domain.ProjectSnapshot
		err      error
	}
	results := make(chan result, len(directions))
	for index, direction := range directions {
		go func(index int, direction string) {
			snapshot, err := o.provider.Build(ctx, request.Prompt+"\n视觉方向："+direction, *request.Plan)
			if err == nil {
				snapshot, err = o.guard.Check(snapshot)
			}
			results <- result{index: index, snapshot: snapshot, err: err}
		}(index, direction.text)
	}
	candidates := make([]domain.RaceCandidate, len(directions))
	valid := make([]bool, len(directions))
	for range directions {
		result := <-results
		if result.err == nil {
			result.err = domain.ValidateBuildContract(*request.Plan, result.snapshot)
		}
		if result.err != nil {
			result.snapshot, result.err = (&ai.FakeProvider{}).Build(ctx, request.Prompt+"\n视觉方向："+directions[result.index].text, *request.Plan)
			if result.err == nil {
				result.snapshot, result.err = o.guard.Check(result.snapshot)
			}
		}
		if result.err != nil {
			continue
		}
		direction := directions[result.index]
		candidates[result.index] = domain.RaceCandidate{ID: direction.id, Direction: direction.text, Snapshot: result.snapshot}
		valid[result.index] = true
	}
	completed := candidates[:0]
	for index := range candidates {
		if valid[index] {
			completed = append(completed, candidates[index])
		}
	}
	if len(completed) == 0 {
		o.sendFailure(ctx, send, "race_failed", "两个 Race 候选均生成失败。")
		return
	}
	send(domain.AgentEvent{Type: "agent.completed", Agent: "alex", Message: "Race 候选已经准备好。"})
	send(domain.AgentEvent{Type: "snapshot.completed", Candidates: completed})
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
