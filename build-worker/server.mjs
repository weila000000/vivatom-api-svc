import { createServer } from "node:http"
import { mkdtemp, mkdir, rm, symlink, writeFile } from "node:fs/promises"
import { spawn } from "node:child_process"
import { randomUUID } from "node:crypto"
import { dirname, join } from "node:path"
import { tmpdir } from "node:os"

const port = Number(process.env.VIVATOM_BUILDER_PORT || 8090)
const token = process.env.VIVATOM_BUILDER_TOKEN || "vivatom-local-builder"
const maxBodyBytes = 2.25 * 1024 * 1024
const maxOutputBytes = 64 * 1024
const timeoutMs = 45_000
const toolchain = "vite@7.3.6+vue@3.5.42"
const workerRoot = dirname(new URL(import.meta.url).pathname)

function log(level, event, fields = {}) {
  const line = JSON.stringify({ time: new Date().toISOString(), level, event, ...fields })
  process[level === "error" ? "stderr" : "stdout"].write(`${line}\n`)
}

function safePath(path) {
  return /^\/src\/[A-Za-z0-9_./-]+\.(vue|ts|js|tsx|jsx|css|json)$/.test(path) && !path.includes("..")
}

async function readJSON(request) {
  const chunks = []
  let size = 0
  for await (const chunk of request) {
    size += chunk.length
    if (size > maxBodyBytes) throw new Error("request_too_large")
    chunks.push(chunk)
  }
  return JSON.parse(Buffer.concat(chunks).toString("utf8"))
}

async function compile(snapshot, requestId) {
  if (!snapshot || !safePath(snapshot.entryFile) || !snapshot.files || snapshot.dependencies?.vue !== "3.5.42") {
    throw new Error("invalid_snapshot")
  }
  const paths = Object.keys(snapshot.files)
  if (!paths.length || paths.length > 80 || paths.some((path) => !safePath(path)) || !paths.includes(snapshot.entryFile)) {
    throw new Error("invalid_snapshot")
  }
  const sourceBytes = Object.values(snapshot.files).reduce((total, source) => total + Buffer.byteLength(String(source)), 0)
  log("info", "compile_started", { requestId, entryFile: snapshot.entryFile, fileCount: paths.length, sourceBytes })
  const directory = await mkdtemp(join(tmpdir(), "vivatom-build-"))
  try {
    await symlink(join(workerRoot, "node_modules"), join(directory, "node_modules"), "dir")
    await writeFile(join(directory, "index.html"), `<div id="app"></div><script type="module" src="${snapshot.entryFile}"></script>`)
    for (const [path, source] of Object.entries(snapshot.files)) {
      if (typeof source !== "string" || Buffer.byteLength(source) > 256 * 1024) throw new Error("invalid_snapshot")
      const target = join(directory, path.slice(1))
      await mkdir(dirname(target), { recursive: true })
      await writeFile(target, source)
    }
    await runVite(directory, snapshot.entryFile, requestId)
  } finally {
    await rm(directory, { recursive: true, force: true })
    log("info", "workspace_removed", { requestId })
  }
}

function runVite(directory, entryFile, requestId) {
  return new Promise((resolve, reject) => {
    const startedAt = Date.now()
    const child = spawn(process.execPath, [join(workerRoot, "runner.mjs"), directory, entryFile], {
      cwd: directory,
      env: { PATH: process.env.PATH, NODE_ENV: "production", HOME: tmpdir() },
      stdio: ["ignore", "pipe", "pipe"],
    })
    log("info", "vite_started", { requestId, pid: child.pid, entryFile })
    let output = ""
    const collect = (chunk) => { if (output.length < maxOutputBytes) output += chunk.toString() }
    child.stdout.on("data", collect)
    child.stderr.on("data", collect)
    const timer = setTimeout(() => {
      log("error", "vite_timeout", { requestId, pid: child.pid, timeoutMs })
      child.kill("SIGKILL")
    }, timeoutMs)
    child.once("error", (error) => {
      clearTimeout(timer)
      log("error", "vite_spawn_failed", { requestId, error: String(error) })
      reject(error)
    })
    child.once("exit", (code) => {
      clearTimeout(timer)
      const durationMs = Date.now() - startedAt
      const buildOutput = output.trim().slice(-4000)
      if (code === 0) {
        log("info", "vite_completed", { requestId, pid: child.pid, code, durationMs, output: buildOutput })
        resolve()
      } else {
        log("error", "vite_failed", { requestId, pid: child.pid, code, durationMs, output: buildOutput })
        reject(new Error(buildOutput || `vite exited with ${code}`))
      }
    })
  })
}

const server = createServer(async (request, response) => {
  const requestId = randomUUID().replaceAll("-", "")
  const requestStartedAt = Date.now()
  const remoteAddress = request.socket.remoteAddress
  log("info", "request_started", { requestId, method: request.method, path: request.url, remoteAddress })
  if (request.method === "GET" && request.url === "/health") {
    response.writeHead(204).end()
    log("info", "request_completed", { requestId, status: 204, durationMs: Date.now() - requestStartedAt })
    return
  }
  if (request.method !== "POST" || request.url !== "/compile") {
    response.writeHead(404).end()
    log("info", "request_completed", { requestId, status: 404, durationMs: Date.now() - requestStartedAt })
    return
  }
  if (request.headers["x-vivatom-builder-token"] !== token) {
    response.writeHead(401).end()
    log("error", "request_rejected", { requestId, status: 401, reason: "invalid_token", durationMs: Date.now() - requestStartedAt })
    return
  }
  try {
    const body = await readJSON(request)
    const startedAt = Date.now()
    await compile(body.snapshot, requestId)
    const payload = JSON.stringify({ data: { toolchain, durationMs: Date.now() - startedAt } })
    response.writeHead(200, { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(payload) }).end(payload)
    log("info", "request_completed", { requestId, status: 200, durationMs: Date.now() - requestStartedAt, toolchain })
  } catch (error) {
    log("error", "compile_failed", { requestId, status: 422, durationMs: Date.now() - requestStartedAt, error: String(error).slice(0, 4000) })
    response.writeHead(422, { "Content-Type": "application/json" }).end('{"error":"compile_failed"}')
  }
}).listen(port, "0.0.0.0", () => {
  log("info", "builder_started", { port, toolchain, maxBodyBytes, maxOutputBytes, timeoutMs })
})

server.on("clientError", (error, socket) => {
  log("error", "client_error", { error: String(error) })
  socket.end("HTTP/1.1 400 Bad Request\r\n\r\n")
})

let shuttingDown = false
for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => {
    if (shuttingDown) return
    shuttingDown = true
    log("info", "shutdown_started", { signal })
    server.close((error) => {
      if (error) {
        log("error", "shutdown_failed", { signal, error: String(error) })
        process.exitCode = 1
      } else {
        log("info", "shutdown_completed", { signal })
      }
    })
  })
}
