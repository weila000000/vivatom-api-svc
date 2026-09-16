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
cp .env.example .env
docker compose up --build
```

本地示例包含显式开发 Token。部署前必须把 `VIVATOM_BUILDER_TOKEN` 替换为至少 16 个字符的随机密钥；未配置时 API、Worker 和 Compose 都会拒绝启动。

Build Worker 向标准输出写入 JSON 日志。启动、请求、Snapshot 元数据、Vite 子进程输出、编译耗时、拒绝原因、临时目录清理和优雅退出使用同一个 `requestId` 关联；源码与认证 Token 不会写入日志。

`GET /api/health/live` 只表示 API 进程仍在运行；`GET /api/health/ready` 会同时检查 SQLite 和 Build Worker。Worker 会继续检查 Vite 工具链和 Artifact Store 读写权限；未启动、工具链缺失、产物卷不可写或健康接口超时时，readiness 返回 `503`，前端会据此提示服务尚未就绪。

Compose 使用 Node 自身执行 Worker 健康探针，不依赖基础镜像中的额外命令。Worker 的停止宽限期为 60 秒，覆盖 45 秒构建超时和工作区清理；API 的停止宽限期为 15 秒，覆盖默认 10 秒优雅停机窗口。

Worker 将控制面和预览面分离：8090 仅供 API 在内部网络调用编译、验证与 readiness，8091 仅公开静态预览。前端通过 `VITE_PREVIEW_BASE_URL` 指向 `http://localhost:8091/preview` 或独立预览域名，公开域名不应反向代理 8090。

## Verification

```bash
go test ./...
go vet ./...
npm audit --omit=dev --prefix build-worker
```
