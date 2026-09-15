import { createServer } from "node:http"
import { mkdtemp, mkdir, rm, symlink, writeFile } from "node:fs/promises"
import { spawn } from "node:child_process"
import { dirname, join } from "node:path"
import { tmpdir } from "node:os"

const port = Number(process.env.VIVATOM_BUILDER_PORT || 8090)
const token = process.env.VIVATOM_BUILDER_TOKEN || "vivatom-local-builder"
const maxBodyBytes = 2.25 * 1024 * 1024
const maxOutputBytes = 64 * 1024
const timeoutMs = 45_000
const workerRoot = dirname(new URL(import.meta.url).pathname)

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

async function compile(snapshot) {
  if (!snapshot || !safePath(snapshot.entryFile) || !snapshot.files || snapshot.dependencies?.vue !== "3.5.42") {
    throw new Error("invalid_snapshot")
  }
  const paths = Object.keys(snapshot.files)
  if (!paths.length || paths.length > 80 || paths.some((path) => !safePath(path)) || !paths.includes(snapshot.entryFile)) {
    throw new Error("invalid_snapshot")
  }
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
    await runVite(directory, snapshot.entryFile)
  } finally {
    await rm(directory, { recursive: true, force: true })
  }
}

function runVite(directory, entryFile) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [join(workerRoot, "runner.mjs"), directory, entryFile], {
      cwd: directory,
      env: { PATH: process.env.PATH, NODE_ENV: "production", HOME: tmpdir() },
      stdio: ["ignore", "pipe", "pipe"],
    })
    let output = ""
    const collect = (chunk) => { if (output.length < maxOutputBytes) output += chunk.toString() }
    child.stdout.on("data", collect)
    child.stderr.on("data", collect)
    const timer = setTimeout(() => child.kill("SIGKILL"), timeoutMs)
    child.once("error", (error) => { clearTimeout(timer); reject(error) })
    child.once("exit", (code) => {
      clearTimeout(timer)
      if (code === 0) resolve()
      else reject(new Error(output.slice(-4000) || `vite exited with ${code}`))
    })
  })
}

createServer(async (request, response) => {
  if (request.method === "GET" && request.url === "/health") {
    response.writeHead(204).end()
    return
  }
  if (request.method !== "POST" || request.url !== "/compile") {
    response.writeHead(404).end()
    return
  }
  if (request.headers["x-vivatom-builder-token"] !== token) {
    response.writeHead(401).end()
    return
  }
  try {
    const body = await readJSON(request)
    await compile(body.snapshot)
    response.writeHead(204).end()
  } catch (error) {
    process.stderr.write(`${new Date().toISOString()} compile_failed ${String(error).slice(0, 4000)}\n`)
    response.writeHead(422, { "Content-Type": "application/json" }).end('{"error":"compile_failed"}')
  }
}).listen(port, "0.0.0.0", () => {
  process.stdout.write(`${new Date().toISOString()} builder_started port=${port}\n`)
})
