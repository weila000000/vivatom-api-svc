# Vivatom API Service

Vivatom 的 Go + Gin 后端。服务负责平台身份、工作区权限、Agent 编排、完整源码快照检查、用量控制、审计日志和生成应用 Runtime。

## Development

```bash
cp .env.example .env
npm ci --prefix build-worker
npm start --prefix build-worker
```

另一个终端启动 API：

```bash
go run ./cmd/api
```

默认地址为 `http://localhost:8080`。

容器方式会同时启动 API 与隔离构建 Worker：

```bash
docker compose up --build
```

## Verification

```bash
go test ./...
go vet ./...
npm audit --omit=dev --prefix build-worker
```
