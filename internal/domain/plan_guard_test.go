package domain

import "testing"

func validBuildPlan() BuildPlan {
	return BuildPlan{ProductSummary: "任务板", TargetUsers: []string{"团队"}, Features: []string{"任务管理"}, Pages: []PlanPage{{Name: "首页", Purpose: "管理任务"}}, FilePlan: []PlanFile{{Path: "/src/App.tsx", Responsibility: "应用入口"}}, DesignDirection: "清晰", AcceptanceChecks: []string{"可以创建任务"}}
}

func TestPlanGuardAcceptsACompletePlan(t *testing.T) {
	if err := ValidateBuildPlan(validBuildPlan()); err != nil {
		t.Fatal(err)
	}
}

func TestPlanGuardRejectsUnsafeAndDuplicateFiles(t *testing.T) {
	plan := validBuildPlan()
	plan.FilePlan = append(plan.FilePlan, PlanFile{Path: "/src/App.tsx", Responsibility: "重复"})
	assertContractCode(t, ValidateBuildPlan(plan), "plan.file_invalid")
	plan = validBuildPlan()
	plan.FilePlan[0].Path = "/src/../secret.ts"
	assertContractCode(t, ValidateBuildPlan(plan), "plan.file_invalid")
}
