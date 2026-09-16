# Vivatom API Service

Vivatom 的 Go + Gin 后端。服务负责平台身份、工作区权限、Agent 编排、完整源码快照检查、用量控制、审计日志和生成应用 Runtime。

## Development

```bash
npm ci --prefix build-worker
npm start --prefix build-worker
```

另一个终端启动 API：

```bash
cp .env.example .env
set -a
source .env
set +a
go run ./cmd/api
```

OpenAI-compatible Provider 默认连接 `https://vibe.linux008.com/v1`。在 `.env` 中配置 `VIVATOM_AI_API_KEY`（或 `OPENAI_API_KEY`）后，`auto` 模式会启用真实 Provider；未配置密钥时使用本地 FakeProvider。

`VIVATOM_AI_MODEL` 是所有角色的默认模型。可以用 `VIVATOM_AI_ANALYST_MODEL`、`VIVATOM_AI_ARCHITECT_MODEL` 和 `VIVATOM_AI_BUILDER_MODEL` 分别覆盖产品分析、方案架构和源码工程模型；空值继承默认模型。

默认地址为 `http://localhost:8080`。

容器方式会同时启动 API 与隔离构建 Worker：

```bash
docker compose up --build
```

Build Worker 向标准输出写入 JSON 日志。启动、请求、Snapshot 元数据、Vite 子进程输出、编译耗时、拒绝原因、临时目录清理和优雅退出使用同一个 `requestId` 关联；源码与认证 Token 不会写入日志。

`GET /api/health/live` 只表示 API 进程仍在运行；`GET /api/health/ready` 会同时检查 SQLite 和 Build Worker。Worker 未启动、退出或健康接口超时时，readiness 返回 `503`，前端会据此提示服务尚未就绪。

## Verification

```bash
go test ./...
go vet ./...
npm audit --omit=dev --prefix build-worker
```
