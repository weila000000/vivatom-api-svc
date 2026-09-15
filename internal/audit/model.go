package audit

import "encoding/json"

type Event struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspaceId"`
	ActorID     string          `json:"actorId"`
	ActorName   string          `json:"actorName"`
	ActorEmail  string          `json:"actorEmail"`
	Action      string          `json:"action"`
	TargetType  string          `json:"targetType"`
	TargetID    string          `json:"targetId"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   string          `json:"createdAt"`
}

type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string { return e.Code }
