# Vivatom API Service

Vivatom 的 Go + Gin Agent 服务。核心闭环是：根据模式制定计划、等待人工批准、生成受限的 React + TypeScript 多文件快照、对话迭代，以及在 Race Mode 中并行生成两个视觉候选。

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

OpenAI-compatible Provider 默认连接 `https://vibe.linux008.com/v1`。在 `.env` 中配置 `VIBE_API_KEY` 后，`auto` 模式会启用 `gpt-6-astra`；未配置密钥或模型调用失败时使用本地模板通道，并通过 SSE 发出 `local_fallback` 警告。

`VIBE_MODEL` 是所有角色的默认模型。原有的 `VIVATOM_AI_*` 变量仍作为兼容别名。

前端通过 `POST /api/agent` 调用服务，请求动作支持 `plan`、`build`、`iterate`、`repair` 和 `race`，响应为 SSE。`mode` 支持 `engineer`、`team` 和 `race`，省略时默认为 `team`。

默认地址为 `http://localhost:8080`。

也可以直接运行仓库脚本：

```bash
./scripts/api.sh       # 只启动 API
./scripts/worker.sh    # 只启动 Build Worker
./scripts/dev.sh       # 同时启动 API 和 Worker
./scripts/full-stack.sh # 同时启动 Web、API 和 Worker
./scripts/test.sh      # 运行全部检查
```

GoLand 或 IntelliJ IDEA 单独打开后端项目后，选择共享运行项 `Vivatom Backend`，即可同时启动 API 与 Build Worker。前端在另一个 IDE 窗口打开 `../vivatom-web`，选择 `Vivatom Frontend` 独立启动。后端脚本会读取可选的 `.env`；未创建时使用 `.env.example` 对应的本地安全默认值。

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
