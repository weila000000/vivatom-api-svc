package ai

import (
	"context"
	"strconv"
	"strings"
	"time"

	"vivatom-api-svc/internal/domain"
)

type Planner interface {
	Plan(ctx context.Context, prompt string) (domain.BuildPlan, error)
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
		ProductType:    "web_app",
		ProductSummary: summary,
		TargetUsers:    []string{"需要完成该任务的实际用户"},
		Features:       []string{"清晰的首页入口", "核心信息浏览", "主要操作流程"},
		Pages: []domain.PlanPage{
			{Name: "首页", Purpose: "说明产品并进入核心功能"},
			{Name: "工作区", Purpose: "完成主要业务操作"},
		},
		FilePlan: []domain.PlanFile{
			{Path: "/src/App.vue", Responsibility: "应用入口和页面布局"},
			{Path: "/src/styles.css", Responsibility: "全局视觉样式"},
		},
		DesignDirection:  "清晰、克制、以工作效率为中心",
		AcceptanceChecks: []string{"主要流程可以完成", "移动端内容不溢出", "操作状态有明确反馈"},
		Backend: domain.BackendSpec{
			Enabled:     false,
			Auth:        "none",
			Collections: []domain.BackendCollection{},
		},
	}, nil
}

func (p *FakeProvider) Build(ctx context.Context, prompt string, plan domain.BuildPlan) (domain.ProjectSnapshot, error) {
	if err := p.wait(ctx); err != nil {
		return domain.ProjectSnapshot{}, err
	}
	return domain.ProjectSnapshot{
		Source: "template", Title: "任务工作台", Summary: strings.TrimSpace(prompt),
		Files: map[string]string{
			"/src/App.vue": `<script setup lang="ts">
import { ref } from "vue"
const tasks = ref(["梳理需求", "完成原型", "邀请成员"])
</script>
<template>
  <main>
    <h1>任务工作台</h1>
    <p>从清晰的下一步开始推进项目。</p>
    <ul><li v-for="task in tasks" :key="task">{{ task }}</li></ul>
  </main>
</template>`,
			"/src/main.ts": `import { createApp } from "vue"
import App from "./App.vue"
import "./styles.css"
createApp(App).mount("#app")`,
			"/src/styles.css": `:root { font-family: sans-serif; color: #17211b; background: #f4f6f2; }
body { margin: 0; }
main { width: min(680px, calc(100% - 32px)); margin: 64px auto; }
li { margin: 12px 0; }`,
		},
		Dependencies: map[string]string{"vue": "^3.5.0"},
		EntryFile:    "/src/main.ts", Backend: plan.Backend,
	}, nil
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
