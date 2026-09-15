package usage

type Summary struct {
	WorkspaceID string `json:"workspaceId"`
	Limit       int    `json:"limit"`
	Used        int    `json:"used"`
	Remaining   int    `json:"remaining"`
}

type Result string

const (
	ResultOK              Result = "ok"
	ResultForbidden       Result = "forbidden"
	ResultExhausted       Result = "exhausted"
	ResultApprovalInvalid Result = "approval_invalid"
)

type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string { return e.Code }
