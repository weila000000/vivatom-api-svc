package team

type Member struct {
	AccountID string `json:"accountId"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	JoinedAt  string `json:"joinedAt"`
}

type Invitation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	Token       string `json:"token"`
	ExpiresAt   string `json:"expiresAt"`
	CreatedAt   string `json:"createdAt"`
}

type StoredInvitation struct {
	Invitation
	TokenHash string
	InvitedBy string
}

type Result string

const (
	ResultOK            Result = "ok"
	ResultForbidden     Result = "forbidden"
	ResultNotFound      Result = "not_found"
	ResultLastOwner     Result = "last_owner"
	ResultEmailMismatch Result = "email_mismatch"
)

type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string { return e.Code }
