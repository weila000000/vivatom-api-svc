import { createServer } from "node:http"
import { constants, createReadStream } from "node:fs"
import { access, mkdtemp, mkdir, readFile, readdir, realpath, rename, rm, stat, symlink, writeFile } from "node:fs/promises"
import { spawn } from "node:child_process"
import { createHash, randomUUID } from "node:crypto"
import { dirname, extname, join, relative, resolve, sep } from "node:path"
import { tmpdir } from "node:os"
import { artifactETag, matchesIfNoneMatch } from "./preview-http.mjs"
import { previewSecurityHeaders } from "./preview-security.mjs"
import { hashSnapshot } from "./snapshot-hash.mjs"
import { verifyToolchain } from "./toolchain.mjs"
import { builderTokenMatches, requireBuilderToken } from "./auth.mjs"

const controlPort = Number(process.env.VIVATOM_BUILDER_PORT || 8090)
const previewPort = Number(process.env.VIVATOM_PREVIEW_PORT || 8091)
const token = requireBuilderToken()
const maxBodyBytes = 2.25 * 1024 * 1024
const maxOutputBytes = 64 * 1024
const maxArtifactFiles = 128
const maxArtifactFileBytes = 4 * 1024 * 1024
const maxArtifactBytes = 8 * 1024 * 1024
const timeoutMs = 45_000
const maxConcurrentBuilds = Number(process.env.VIVATOM_BUILDER_CONCURRENCY || 2)
const workerRoot = dirname(new URL(import.meta.url).pathname)
const artifactRoot = resolve(process.env.VIVATOM_ARTIFACT_ROOT || join(workerRoot, ".data", "artifacts"))
const artifactManifestCache = new Map()
const maxCachedManifests = 256
let activeBuilds = 0

if (!Number.isInteger(maxConcurrentBuilds) || maxConcurrentBuilds < 1 || maxConcurrentBuilds > 16) {
  throw new Error("VIVATOM_BUILDER_CONCURRENCY must be an integer between 1 and 16")
}
if (![controlPort, previewPort].every((value) => Number.isInteger(value) && value > 0 && value < 65536) || controlPort === previewPort) {
  throw new Error("builder and preview ports must be distinct valid ports")
}

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

async function checkReady() {
  await mkdir(artifactRoot, { recursive: true })
  const modules = await stat(join(workerRoot, "node_modules"))
  if (!modules.isDirectory()) throw new Error("toolchain_not_found")
  await Promise.all([
    access(join(workerRoot, "runner.mjs"), constants.R_OK),
    access(artifactRoot, constants.R_OK | constants.W_OK),
    verifyToolchain(workerRoot),
  ])
}

async function inspectArtifact(root) {
  const hash = createHash("sha256")
  const files = new Map()
  let fileCount = 0
  let totalBytes = 0
  let hasIndex = false
  async function visit(directory) {
    const entries = await readdir(directory, { withFileTypes: true })
    entries.sort((left, right) => left.name < right.name ? -1 : left.name > right.name ? 1 : 0)
    for (const entry of entries) {
      const path = join(directory, entry.name)
      if (entry.isDirectory()) {
        await visit(path)
        continue
      }
      if (!entry.isFile()) throw new Error("invalid_artifact")
      const artifactPath = relative(root, path).split(sep).join("/")
      const metadata = await stat(path)
      fileCount++
      totalBytes += metadata.size
      if (fileCount > maxArtifactFiles) throw new Error("artifact_too_many_files")
      if (metadata.size > maxArtifactFileBytes) throw new Error("artifact_file_too_large")
      if (totalBytes > maxArtifactBytes) throw new Error("artifact_too_large")
      if (artifactPath === "index.html") hasIndex = true
      hash.update(`${Buffer.byteLength(artifactPath)}:`)
      hash.update(artifactPath)
      hash.update(`${metadata.size}:`)
      const fileHash = createHash("sha256")
      let streamedBytes = 0
      for await (const chunk of createReadStream(path)) {
        streamedBytes += chunk.length
        if (streamedBytes > metadata.size) throw new Error("artifact_changed_during_hash")
        hash.update(chunk)
        fileHash.update(chunk)
      }
      if (streamedBytes !== metadata.size) throw new Error("artifact_changed_during_hash")
      files.set(artifactPath, { size: metadata.size, hash: fileHash.digest("hex") })
    }
  }
  await visit(root)
  if (!hasIndex) throw new Error("artifact_entry_missing")
  return { artifactId: hash.digest("hex"), fileCount, totalBytes, files }
}

function rememberArtifact(artifact) {
  artifactManifestCache.delete(artifact.artifactId)
  artifactManifestCache.set(artifact.artifactId, artifact)
  while (artifactManifestCache.size > maxCachedManifests) {
    artifactManifestCache.delete(artifactManifestCache.keys().next().value)
  }
}

async function persistArtifact(source, artifact, requestId) {
  const { artifactId, fileCount, totalBytes } = artifact
  await mkdir(artifactRoot, { recursive: true })
  const destination = join(artifactRoot, artifactId)
  try {
    await rename(source, destination)
    rememberArtifact(artifact)
    log("info", "artifact_persisted", { requestId, artifactId, fileCount, totalBytes })
  } catch (error) {
    if (error?.code !== "EEXIST" && error?.code !== "ENOTEMPTY") throw error
    const existing = await inspectArtifact(destination)
    if (existing.artifactId !== artifactId) throw new Error("artifact_integrity_mismatch")
    rememberArtifact(existing)
    log("info", "artifact_reused", { requestId, artifactId, fileCount: existing.fileCount, totalBytes: existing.totalBytes })
  }
}

async function verifyArtifact(artifactId) {
  if (!/^[a-f0-9]{64}$/.test(artifactId)) throw new Error("invalid_artifact_id")
  const root = join(artifactRoot, artifactId)
  if (!(await stat(root)).isDirectory()) throw new Error("artifact_not_found")
  const artifact = await inspectArtifact(root)
  if (artifact.artifactId !== artifactId) throw new Error("artifact_integrity_mismatch")
  rememberArtifact(artifact)
  return artifact
}

async function artifactManifest(artifactId) {
  const cached = artifactManifestCache.get(artifactId)
  if (!cached) return verifyArtifact(artifactId)
  rememberArtifact(cached)
  return cached
}

async function compile(snapshot, snapshotHash, requestId, signal) {
  if (signal.aborted) throw new Error("request_aborted")
  const dependencies = snapshot?.dependencies && Object.entries(snapshot.dependencies)
  if (!snapshot || !safePath(snapshot.entryFile) || !snapshot.files || dependencies?.length !== 1 || snapshot.dependencies.vue !== "3.5.42") {
    throw new Error("invalid_snapshot")
  }
  if (!/^[a-f0-9]{64}$/.test(snapshotHash)) throw new Error("invalid_snapshot_hash")
  if (hashSnapshot(snapshot) !== snapshotHash) throw new Error("snapshot_hash_mismatch")
  const toolchain = await verifyToolchain(workerRoot)
  const paths = Object.keys(snapshot.files)
  if (!paths.length || paths.length > 80 || paths.some((path) => !safePath(path)) || !paths.includes(snapshot.entryFile)) {
    throw new Error("invalid_snapshot")
  }
  const sourceBytes = Object.values(snapshot.files).reduce((total, source) => total + Buffer.byteLength(String(source)), 0)
  log("info", "compile_started", { requestId, snapshotHash, entryFile: snapshot.entryFile, fileCount: paths.length, sourceBytes })
  const directory = await mkdtemp(join(tmpdir(), "vivatom-build-"))
  try {
    const canonicalDirectory = await realpath(directory)
    await symlink(join(workerRoot, "node_modules"), join(directory, "node_modules"), "dir")
    await writeFile(join(directory, "index.html"), `<div id="app"></div><script type="module" src="${snapshot.entryFile}"></script>`)
    for (const [path, source] of Object.entries(snapshot.files)) {
      if (signal.aborted) throw new Error("request_aborted")
      if (typeof source !== "string" || Buffer.byteLength(source) > 256 * 1024) throw new Error("invalid_snapshot")
      const target = join(directory, path.slice(1))
      await mkdir(dirname(target), { recursive: true })
      await writeFile(target, source)
    }
    await runVite(canonicalDirectory, canonicalDirectory, snapshot.entryFile, requestId, signal)
    if (signal.aborted) throw new Error("request_aborted")
    const dist = join(directory, "dist")
    const artifact = await inspectArtifact(dist)
    await persistArtifact(dist, artifact, requestId)
    return { artifactId: artifact.artifactId, toolchain }
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
  let artifactPath = requested || "index.html"
  let servingIndex = !requested
  try {
    if (!(await stat(target)).isFile()) throw new Error("not_file")
  } catch {
    if (extname(requested)) {
      response.writeHead(404).end()
      return true
    }
    target = join(root, "index.html")
    artifactPath = "index.html"
    servingIndex = true
    try {
      if (!(await stat(target)).isFile()) throw new Error("not_file")
    } catch {
      response.writeHead(404).end()
      return true
    }
  }
  const manifest = await artifactManifest(artifactId)
  const expected = manifest.files.get(artifactPath)
  if (!expected) throw new Error("artifact_file_untrusted")
  const content = await readFile(target)
  if (content.length !== expected.size || createHash("sha256").update(content).digest("hex") !== expected.hash) {
    throw new Error("artifact_file_integrity_mismatch")
  }
  const etag = artifactETag(expected.hash)
  const headers = {
    "Cache-Control": servingIndex ? "no-cache" : "public, max-age=31536000, immutable",
    "Content-Type": contentTypes[extname(target)] || "application/octet-stream",
    ETag: etag,
    ...previewSecurityHeaders,
  }
  if (matchesIfNoneMatch(request.headers["if-none-match"], etag)) {
    response.writeHead(304, headers).end()
    return true
  }
  response.writeHead(200, { ...headers, "Content-Length": content.length })
  response.end(request.method === "HEAD" ? undefined : content)
  return true
}

function runVite(directory, canonicalDirectory, entryFile, requestId, signal) {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(new Error("request_aborted"))
      return
    }
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
    let settled = false
    const finish = (callback) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      signal.removeEventListener("abort", cancel)
      callback()
    }
    const cancel = () => {
      log("info", "vite_cancelled", { requestId, pid: child.pid, durationMs: Date.now() - startedAt })
      child.kill("SIGKILL")
    }
    const timer = setTimeout(() => {
      log("error", "vite_timeout", { requestId, pid: child.pid, timeoutMs })
      child.kill("SIGKILL")
    }, timeoutMs)
    signal.addEventListener("abort", cancel, { once: true })
    child.once("error", (error) => {
      finish(() => {
        log("error", "vite_spawn_failed", { requestId, error: String(error) })
        reject(error)
      })
    })
    child.once("exit", (code) => {
      finish(() => {
        const durationMs = Date.now() - startedAt
        const buildOutput = sanitizeDiagnostic(output, [directory, canonicalDirectory])
        if (signal.aborted) {
          reject(new Error("request_aborted"))
        } else if (code === 0) {
          log("info", "vite_completed", { requestId, pid: child.pid, code, durationMs, output: buildOutput })
          resolve()
        } else {
          log("error", "vite_failed", { requestId, pid: child.pid, code, durationMs, output: buildOutput })
          reject(new Error(buildOutput || `vite exited with ${code}`))
        }
      })
    })
  })
}

const controlServer = createServer(async (request, response) => {
  const requestId = randomUUID().replaceAll("-", "")
  const requestStartedAt = Date.now()
  const remoteAddress = request.socket.remoteAddress
  const pathname = new URL(request.url || "/", "http://builder.local").pathname
  log("info", "request_started", { requestId, method: request.method, path: request.url, remoteAddress })
  if ((request.method === "GET" || request.method === "HEAD") && request.url === "/health") {
    try {
      await checkReady()
      response.writeHead(204).end()
      log("info", "readiness_passed", { requestId, status: 204, durationMs: Date.now() - requestStartedAt })
    } catch (error) {
      response.writeHead(503).end()
      log("error", "readiness_failed", { requestId, status: 503, durationMs: Date.now() - requestStartedAt, error: sanitizeDiagnostic(error) })
    }
    return
  }
  if (request.method !== "POST" || (request.url !== "/compile" && request.url !== "/verify")) {
    response.writeHead(404).end()
    log("info", "request_completed", { requestId, status: 404, durationMs: Date.now() - requestStartedAt })
    return
  }
  if (!builderTokenMatches(token, request.headers["x-vivatom-builder-token"])) {
    response.writeHead(401).end()
    log("error", "request_rejected", { requestId, status: 401, reason: "invalid_token", durationMs: Date.now() - requestStartedAt })
    return
  }
  const compiling = request.url === "/compile"
  if (compiling && activeBuilds >= maxConcurrentBuilds) {
    request.resume()
    const payload = JSON.stringify({ error: "builder_busy" })
    response.writeHead(503, { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(payload), "Retry-After": "1" }).end(payload)
    log("error", "compile_rejected", { requestId, status: 503, reason: "builder_busy", activeBuilds, maxConcurrentBuilds, durationMs: Date.now() - requestStartedAt })
    return
  }
  if (compiling) {
    activeBuilds++
    log("info", "build_slot_acquired", { requestId, activeBuilds, maxConcurrentBuilds })
  }
  const cancellation = compiling ? new AbortController() : undefined
  const cancelOnDisconnect = () => {
    if (cancellation && !response.writableEnded && !cancellation.signal.aborted) {
      cancellation.abort()
      log("info", "request_cancelled", { requestId, activeBuilds, durationMs: Date.now() - requestStartedAt })
    }
  }
  request.once("aborted", cancelOnDisconnect)
  response.once("close", cancelOnDisconnect)
  try {
    const body = await readJSON(request)
    if (request.url === "/verify") {
      const artifact = await verifyArtifact(body.artifactId)
      response.writeHead(204).end()
      log("info", "artifact_verified", { requestId, artifactId: body.artifactId, fileCount: artifact.fileCount, totalBytes: artifact.totalBytes, durationMs: Date.now() - requestStartedAt })
      return
    }
    const startedAt = Date.now()
    const { artifactId, toolchain } = await compile(body.snapshot, body.snapshotHash, requestId, cancellation.signal)
    const payload = JSON.stringify({ data: { toolchain, durationMs: Date.now() - startedAt, artifactId } })
    response.writeHead(200, { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(payload) }).end(payload)
    log("info", "request_completed", { requestId, status: 200, durationMs: Date.now() - requestStartedAt, toolchain })
  } catch (error) {
    if (cancellation?.signal.aborted) {
      log("info", "compile_cancelled", { requestId, durationMs: Date.now() - requestStartedAt })
      if (!response.destroyed && !response.writableEnded) response.writeHead(499).end()
      return
    }
    const diagnostic = sanitizeDiagnostic(error)
    const verifying = request.url === "/verify"
    const errorCode = verifying ? "artifact_unavailable" : "compile_failed"
    log("error", verifying ? "artifact_verification_failed" : "compile_failed", { requestId, status: 422, durationMs: Date.now() - requestStartedAt, error: diagnostic })
    const payload = JSON.stringify({ error: errorCode, diagnostic })
    response.writeHead(422, { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(payload) }).end(payload)
  } finally {
    request.removeListener("aborted", cancelOnDisconnect)
    response.removeListener("close", cancelOnDisconnect)
    if (compiling) {
      activeBuilds--
      log("info", "build_slot_released", { requestId, activeBuilds, maxConcurrentBuilds })
    }
  }
}).listen(controlPort, "0.0.0.0", () => {
  log("info", "builder_started", { controlPort, previewPort, artifactRoot, maxBodyBytes, maxOutputBytes, maxArtifactFiles, maxArtifactFileBytes, maxArtifactBytes, timeoutMs, maxConcurrentBuilds })
})

const previewServer = createServer(async (request, response) => {
  const requestId = randomUUID().replaceAll("-", "")
  const requestStartedAt = Date.now()
  const pathname = new URL(request.url || "/", "http://preview.local").pathname
  log("info", "preview_request_started", { requestId, method: request.method, path: request.url, remoteAddress: request.socket.remoteAddress })
  if ((request.method === "GET" || request.method === "HEAD") && request.url === "/health") {
    try {
      await access(artifactRoot, constants.R_OK)
      response.writeHead(204).end()
      log("info", "preview_readiness_passed", { requestId, status: 204, durationMs: Date.now() - requestStartedAt })
    } catch (error) {
      response.writeHead(503).end()
      log("error", "preview_readiness_failed", { requestId, status: 503, durationMs: Date.now() - requestStartedAt, error: sanitizeDiagnostic(error) })
    }
    return
  }
  if (request.method === "GET" || request.method === "HEAD") {
    try {
      if (await serveArtifact(request, response, pathname)) {
        log("info", "preview_served", { requestId, path: pathname, durationMs: Date.now() - requestStartedAt })
        return
      }
    } catch (error) {
      response.writeHead(503, { "Cache-Control": "no-store" }).end()
      log("error", "preview_integrity_failed", { requestId, path: pathname, status: 503, durationMs: Date.now() - requestStartedAt, error: sanitizeDiagnostic(error) })
      return
    }
  }
  response.writeHead(404).end()
  log("info", "preview_request_completed", { requestId, status: 404, durationMs: Date.now() - requestStartedAt })
}).listen(previewPort, "0.0.0.0", () => {
  log("info", "preview_started", { previewPort, artifactRoot })
})

for (const [name, httpServer] of [["control", controlServer], ["preview", previewServer]]) {
  httpServer.on("clientError", (error, socket) => {
    log("error", "client_error", { server: name, error: String(error) })
    socket.end("HTTP/1.1 400 Bad Request\r\n\r\n")
  })
}

function closeServer(server) {
  return new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()))
}

let shuttingDown = false
for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, async () => {
    if (shuttingDown) return
    shuttingDown = true
    log("info", "shutdown_started", { signal })
    try {
      await Promise.all([closeServer(controlServer), closeServer(previewServer)])
      log("info", "shutdown_completed", { signal })
    } catch (error) {
      log("error", "shutdown_failed", { signal, error: String(error) })
      process.exitCode = 1
    }
  })
}
