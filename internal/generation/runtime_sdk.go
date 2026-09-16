package generation

import (
	"encoding/json"

	"vivatom-api-svc/internal/domain"
)

const RuntimeSDKPath = "/src/vivatom-runtime.ts"

func InjectRuntimeSDK(snapshot domain.ProjectSnapshot, projectID string) domain.ProjectSnapshot {
	if !snapshot.Backend.Enabled {
		return snapshot
	}
	files := make(map[string]string, len(snapshot.Files)+1)
	for path, source := range snapshot.Files {
		files[path] = source
	}
	encodedProjectID, _ := json.Marshal(projectID)
	files[RuntimeSDKPath] = runtimeSDKSource(string(encodedProjectID))
	snapshot.Files = files
	return snapshot
}

func runtimeSDKSource(projectID string) string {
	return `const projectId = ` + projectID + `
const requestType = "vivatom:runtime-request"
const responseType = "vivatom:runtime-response"
const readyType = "vivatom:runtime-ready"
const sessionKey = "vivatom-session-" + projectId
let sequence = 0
let memorySession: string | undefined

window.parent.postMessage({ type: readyType, projectId }, "*")

function readSession() {
  try { return localStorage.getItem(sessionKey) ?? memorySession } catch { return memorySession }
}

function writeSession(token?: string) {
  memorySession = token
  try {
    if (token) localStorage.setItem(sessionKey, token)
    else localStorage.removeItem(sessionKey)
  } catch {}
}

export class RuntimeClientError extends Error {
  constructor(readonly code: string, message: string, readonly status: number) {
    super(message)
    this.name = "RuntimeClientError"
  }
}

function request<T>(path: string, method = "GET", body?: unknown): Promise<T> {
  return new Promise((resolve, reject) => {
    const id = Date.now().toString(36) + "-" + (++sequence).toString(36)
    const timeout = window.setTimeout(() => finish(() => reject(new RuntimeClientError("timeout", "请求超时", 408))), 15000)
    const finish = (run: () => void) => {
      window.clearTimeout(timeout)
      window.removeEventListener("message", receive)
      run()
    }
    const receive = (event: MessageEvent) => {
      if (event.source !== window.parent || event.data?.type !== responseType || event.data?.id !== id) return
      finish(() => {
        const payload = event.data.payload
        if (!event.data.ok || payload?.error) reject(new RuntimeClientError(payload?.error?.code ?? "runtime_unavailable", payload?.error?.message ?? "项目数据服务不可用", event.data.status))
        else if (!payload || !("data" in payload)) reject(new RuntimeClientError("invalid_response", "数据服务响应无效", event.data.status))
        else resolve(payload.data as T)
      })
    }
    window.addEventListener("message", receive)
    window.parent.postMessage({ type: requestType, id, projectId, path, method, body, sessionToken: readSession() }, "*")
  })
}

async function authenticate(action: "register" | "login", email: string, password: string) {
  const result = await request<{ user: unknown; token: string; expiresAt: string }>("/auth/" + action, "POST", { email, password })
  writeSession(result.token)
  return result
}

export const vivatomRuntime = {
  register: (email: string, password: string) => authenticate("register", email, password),
  login: (email: string, password: string) => authenticate("login", email, password),
  me: () => request("/auth/me"),
  logout: async () => { await request("/auth/logout", "POST"); writeSession() },
  list: <T = Record<string, unknown>>(collection: string) => request<T[]>("/collections/" + encodeURIComponent(collection)),
  create: <T = Record<string, unknown>>(collection: string, data: Record<string, unknown>) => request<T>("/collections/" + encodeURIComponent(collection), "POST", data),
  update: <T = Record<string, unknown>>(collection: string, id: string, data: Record<string, unknown>) => request<T>("/collections/" + encodeURIComponent(collection) + "/" + encodeURIComponent(id), "PATCH", data),
  remove: (collection: string, id: string) => request("/collections/" + encodeURIComponent(collection) + "/" + encodeURIComponent(id), "DELETE"),
}
`
}
