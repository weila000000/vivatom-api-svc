import { createServer } from "node:http"
import { createReadStream } from "node:fs"
import { mkdtemp, mkdir, realpath, rename, rm, stat, symlink, writeFile } from "node:fs/promises"
import { spawn } from "node:child_process"
import { randomUUID } from "node:crypto"
import { dirname, extname, join, resolve } from "node:path"
import { tmpdir } from "node:os"

const port = Number(process.env.VIVATOM_BUILDER_PORT || 8090)
const token = process.env.VIVATOM_BUILDER_TOKEN || "vivatom-local-builder"
const maxBodyBytes = 2.25 * 1024 * 1024
const maxOutputBytes = 64 * 1024
const timeoutMs = 45_000
const toolchain = "vite@7.3.6+vue@3.5.42"
const workerRoot = dirname(new URL(import.meta.url).pathname)
const artifactRoot = resolve(process.env.VIVATOM_ARTIFACT_ROOT || join(workerRoot, ".data", "artifacts"))

function log(level, event, fields = {}) {
  const line = JSON.stringify({ time: new Date().toISOString(), level, event, ...fields })
  process[level === "error" ? "stderr" : "stdout"].write(`${line}\n`)
}

function safePath(path) {
  return /^\/src\/[A-Za-z0-9_./-]+\.(vue|ts|js|tsx|jsx|css|json)$/.test(path) && !path.includes("..")
}

function sanitizeDiagnostic(value, workspaces = []) {
  let diagnostic = String(value).replaceAll(/\x1B\[[0-?]*[ -/]*[@-~]/g, "")
  for (const workspace of workspaces.filter(Boolean).sort((left, right) => right.length - left.length)) {
    diagnostic = diagnostic.replaceAll(workspace, "<workspace>")
  }
  return diagnostic.replaceAll(workerRoot, "<worker>").trim().slice(-4000)
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

async function compile(snapshot, artifactId, requestId) {
  if (!snapshot || !safePath(snapshot.entryFile) || !snapshot.files || snapshot.dependencies?.vue !== "3.5.42") {
    throw new Error("invalid_snapshot")
  }
  if (!/^[a-f0-9]{64}$/.test(artifactId)) throw new Error("invalid_artifact_id")
  const paths = Object.keys(snapshot.files)
  if (!paths.length || paths.length > 80 || paths.some((path) => !safePath(path)) || !paths.includes(snapshot.entryFile)) {
    throw new Error("invalid_snapshot")
  }
  const sourceBytes = Object.values(snapshot.files).reduce((total, source) => total + Buffer.byteLength(String(source)), 0)
  log("info", "compile_started", { requestId, entryFile: snapshot.entryFile, fileCount: paths.length, sourceBytes })
  const directory = await mkdtemp(join(tmpdir(), "vivatom-build-"))
  try {
    const canonicalDirectory = await realpath(directory)
    await symlink(join(workerRoot, "node_modules"), join(directory, "node_modules"), "dir")
    await writeFile(join(directory, "index.html"), `<div id="app"></div><script type="module" src="${snapshot.entryFile}"></script>`)
    for (const [path, source] of Object.entries(snapshot.files)) {
      if (typeof source !== "string" || Buffer.byteLength(source) > 256 * 1024) throw new Error("invalid_snapshot")
      const target = join(directory, path.slice(1))
      await mkdir(dirname(target), { recursive: true })
      await writeFile(target, source)
    }
    await runVite(canonicalDirectory, canonicalDirectory, snapshot.entryFile, requestId)
    await mkdir(artifactRoot, { recursive: true })
    const destination = join(artifactRoot, artifactId)
    await rm(destination, { recursive: true, force: true })
    await rename(join(directory, "dist"), destination)
    log("info", "artifact_persisted", { requestId, artifactId })
  } finally {
    await rm(directory, { recursive: true, force: true })
    log("info", "workspace_removed", { requestId })
  }
}

const contentTypes = {
  ".css": "text/css; charset=utf-8",
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".map": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".txt": "text/plain; charset=utf-8",
  ".webp": "image/webp",
}

async function serveArtifact(request, response, pathname) {
  const match = pathname.match(/^\/preview\/([a-f0-9]{64})(?:\/(.*))?$/)
  if (!match) return false
  const [, artifactId, requested = ""] = match
  if (!requested && !pathname.endsWith("/")) {
    response.writeHead(308, { Location: `${pathname}/` }).end()
    return true
  }
  if (requested.includes("..") || !/^[A-Za-z0-9_./-]*$/.test(requested)) {
    response.writeHead(404).end()
    return true
  }
  const root = join(artifactRoot, artifactId)
  let target = join(root, requested || "index.html")
  let servingIndex = !requested
  try {
    if (!(await stat(target)).isFile()) throw new Error("not_file")
  } catch {
    if (extname(requested)) {
      response.writeHead(404).end()
      return true
    }
    target = join(root, "index.html")
    servingIndex = true
    try {
      if (!(await stat(target)).isFile()) throw new Error("not_file")
    } catch {
      response.writeHead(404).end()
      return true
    }
  }
  response.writeHead(200, {
    "Cache-Control": servingIndex ? "no-cache" : "public, max-age=31536000, immutable",
    "Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors *",
    "Content-Type": contentTypes[extname(target)] || "application/octet-stream",
    "Cross-Origin-Resource-Policy": "cross-origin",
    "X-Content-Type-Options": "nosniff",
  })
  createReadStream(target).pipe(response)
  return true
}

function runVite(directory, canonicalDirectory, entryFile, requestId) {
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
      const buildOutput = sanitizeDiagnostic(output, [directory, canonicalDirectory])
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
  const pathname = new URL(request.url || "/", "http://builder.local").pathname
  log("info", "request_started", { requestId, method: request.method, path: request.url, remoteAddress })
  if (request.method === "GET" && request.url === "/health") {
    response.writeHead(204).end()
    log("info", "request_completed", { requestId, status: 204, durationMs: Date.now() - requestStartedAt })
    return
  }
  if (request.method === "GET" && await serveArtifact(request, response, pathname)) {
    log("info", "preview_served", { requestId, path: pathname, durationMs: Date.now() - requestStartedAt })
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
    await compile(body.snapshot, body.artifactId, requestId)
    const payload = JSON.stringify({ data: { toolchain, durationMs: Date.now() - startedAt, artifactId: body.artifactId } })
    response.writeHead(200, { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(payload) }).end(payload)
    log("info", "request_completed", { requestId, status: 200, durationMs: Date.now() - requestStartedAt, toolchain })
  } catch (error) {
    const diagnostic = sanitizeDiagnostic(error)
    log("error", "compile_failed", { requestId, status: 422, durationMs: Date.now() - requestStartedAt, error: diagnostic })
    const payload = JSON.stringify({ error: "compile_failed", diagnostic })
    response.writeHead(422, { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(payload) }).end(payload)
  }
}).listen(port, "0.0.0.0", () => {
  log("info", "builder_started", { port, toolchain, artifactRoot, maxBodyBytes, maxOutputBytes, timeoutMs })
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
