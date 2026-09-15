package identity

type Account struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

type StoredAccount struct {
	Account
	PasswordSalt string
	PasswordHash string
}

type Workspace struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
}

type Session struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

type Authentication struct {
	User       Account     `json:"user"`
	Session    Session     `json:"session"`
	Workspaces []Workspace `json:"workspaces"`
}

type Registration struct {
	Email         string
	Password      string
	Name          string
	WorkspaceName string
}

type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string { return e.Code }
