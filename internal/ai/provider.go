package ai

import (
	"context"
	"path"
	"strconv"
	"strings"
	"time"

	"vivatom-api-svc/internal/domain"
)

type Planner interface {
	Plan(ctx context.Context, prompt string) (domain.BuildPlan, error)
}

type CollaborativePlanner interface {
	AnalyzeRequirements(ctx context.Context, prompt string) (domain.RequirementBrief, error)
	PlanFromBrief(ctx context.Context, prompt string, brief domain.RequirementBrief) (domain.BuildPlan, error)
}

type Builder interface {
	Build(ctx context.Context, prompt string, plan domain.BuildPlan) (domain.ProjectSnapshot, error)
	Revise(ctx context.Context, action domain.AgentAction, instruction string, snapshot domain.ProjectSnapshot) (domain.ProjectSnapshot, error)
}

type Provider interface {
	Planner
	Builder
}

type FakeProvider struct {
	Delay time.Duration
}

func NewFakeProvider() *FakeProvider {
	return &FakeProvider{Delay: 350 * time.Millisecond}
}

func (p *FakeProvider) Plan(ctx context.Context, prompt string) (domain.BuildPlan, error) {
	if err := p.wait(ctx); err != nil {
		return domain.BuildPlan{}, err
	}

	summary := strings.TrimSpace(prompt)
	return domain.BuildPlan{
		ProductSummary: summary,
		TargetUsers:    []string{"需要完成该任务的实际用户"},
		Features:       []string{"清晰的首页入口", "核心信息浏览", "主要操作流程"},
		Pages: []domain.PlanPage{
			{Name: "首页", Purpose: "说明产品并进入核心功能"},
			{Name: "工作区", Purpose: "完成主要业务操作"},
		},
		FilePlan: []domain.PlanFile{
			{Path: "/src/App.tsx", Responsibility: "应用入口、页面布局和核心交互"},
			{Path: "/src/main.tsx", Responsibility: "React 挂载入口"},
			{Path: "/src/styles.css", Responsibility: "全局视觉样式"},
		},
		DesignDirection:  "清晰、克制、以工作效率为中心",
		AcceptanceChecks: []string{"主要流程可以完成", "移动端内容不溢出", "操作状态有明确反馈"},
	}, nil
}

func (p *FakeProvider) Build(ctx context.Context, prompt string, plan domain.BuildPlan) (domain.ProjectSnapshot, error) {
	if err := p.wait(ctx); err != nil {
		return domain.ProjectSnapshot{}, err
	}
	title, description := templateIdentity(prompt)
	files := map[string]string{
		"/src/App.tsx": `import { useEffect, useState } from "react";
import "./styles.css";

const storageKey = "vivatom-task-workspace-tasks";
export default function App() {
  const [tasks, setTasks] = useState<string[]>(() => JSON.parse(localStorage.getItem(storageKey) || '["梳理需求","完成原型","邀请成员"]'));
  const [draft, setDraft] = useState("");
  useEffect(() => localStorage.setItem(storageKey, JSON.stringify(tasks)), [tasks]);
  return <main><h1>` + title + `</h1><p>` + description + `</p><form onSubmit={(event) => { event.preventDefault(); if (draft.trim()) { setTasks([...tasks, draft.trim()]); setDraft(""); } }}><input value={draft} onChange={(event) => setDraft(event.target.value)} placeholder="添加记录"/><button>添加</button></form><ul>{tasks.map((task) => <li key={task}>{task}</li>)}</ul></main>;
}`,
		"/src/main.tsx": `import React from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
createRoot(document.getElementById("root")!).render(<React.StrictMode><App /></React.StrictMode>);`,
		"/src/styles.css": `:root { font-family: Inter, system-ui, sans-serif; color: #17211b; background: #f4f6f2; }
body { margin: 0; }
main { width: min(680px, calc(100% - 32px)); margin: 64px auto; }
form { display: flex; gap: 8px; } input { flex: 1; padding: 10px; } button { padding: 10px 16px; }
li { margin: 12px 0; }`,
	}
	for _, planned := range plan.FilePlan {
		if _, exists := files[planned.Path]; exists {
			continue
		}
		switch path.Ext(planned.Path) {
		case ".css":
			files[planned.Path] = "/* Local fallback styles. */\n"
		case ".json":
			files[planned.Path] = "{}\n"
		default:
			files[planned.Path] = "export {};\n"
		}
	}
	return domain.ProjectSnapshot{
		Source: "template", Title: title, Summary: strings.TrimSpace(prompt),
		Files:        files,
		Dependencies: map[string]string{"react": "18.3.1", "react-dom": "18.3.1"},
		EntryFile:    "/src/App.tsx",
	}, nil
}

func templateIdentity(prompt string) (string, string) {
	normalized := strings.ToLower(prompt)
	switch {
	case strings.Contains(normalized, "dashboard"), strings.Contains(normalized, "仪表盘"), strings.Contains(normalized, "分析"):
		return "数据分析 Dashboard", "查看关键指标，并记录下一步行动。"
	case strings.Contains(normalized, "directory"), strings.Contains(normalized, "目录"), strings.Contains(normalized, "名录"):
		return "资源目录", "浏览、筛选并维护常用资源。"
	case strings.Contains(normalized, "form"), strings.Contains(normalized, "tracker"), strings.Contains(normalized, "表单"), strings.Contains(normalized, "追踪"):
		return "记录追踪器", "提交记录并持续跟踪处理状态。"
	default:
		return "现代产品主页", "清晰呈现价值，并引导用户完成主要操作。"
	}
}

func (p *FakeProvider) Revise(ctx context.Context, action domain.AgentAction, instruction string, snapshot domain.ProjectSnapshot) (domain.ProjectSnapshot, error) {
	if err := p.wait(ctx); err != nil {
		return domain.ProjectSnapshot{}, err
	}
	revised := snapshot
	revised.Files = make(map[string]string, len(snapshot.Files)+1)
	for path, content := range snapshot.Files {
		revised.Files[path] = content
	}
	revised.Dependencies = make(map[string]string, len(snapshot.Dependencies))
	for name, version := range snapshot.Dependencies {
		revised.Dependencies[name] = version
	}
	revised.Source = "template"
	revised.Summary = strings.TrimSpace(instruction)
	revised.Files["/src/revision-note.ts"] = `export const revisionNote = ` + strconv.Quote(string(action)+": "+strings.TrimSpace(instruction))
	return revised, nil
}

func (p *FakeProvider) wait(ctx context.Context) error {
	if p.Delay <= 0 {
		return nil
	}
	timer := time.NewTimer(p.Delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
