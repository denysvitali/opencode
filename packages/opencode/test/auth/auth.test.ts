import { beforeEach, expect, test } from "bun:test"
import fs from "fs/promises"
import path from "path"
import { Auth } from "../../src/auth"
import { Global } from "../../src/global"

const filepath = path.join(Global.Path.data, "auth.json")

beforeEach(async () => {
  await fs.rm(filepath, { force: true })
})

test("set sanitizes api key line breaks and whitespace", async () => {
  await Auth.set("openrouter", {
    type: "api",
    key: "  sk-test-abc\n123\r\n  ",
  })

  const saved = await Bun.file(filepath).json()
  expect(saved.openrouter.key).toBe("sk-test-abc123")

  const loaded = await Auth.get("openrouter")
  expect(loaded).toBeDefined()
  expect(loaded!.type).toBe("api")
  if (loaded!.type === "api") expect(loaded.key).toBe("sk-test-abc123")
})

test("all self-heals malformed persisted api key", async () => {
  await Bun.write(
    filepath,
    JSON.stringify(
      {
        openrouter: {
          type: "api",
          key: "sk-or-v1-a\nbc123\r\n",
        },
      },
      null,
      2,
    ),
  )

  const result = await Auth.all()
  expect(result.openrouter).toBeDefined()
  expect(result.openrouter.type).toBe("api")
  if (result.openrouter.type === "api") expect(result.openrouter.key).toBe("sk-or-v1-abc123")

  const rewritten = await Bun.file(filepath).json()
  expect(rewritten.openrouter.key).toBe("sk-or-v1-abc123")
})

test("all leaves oauth entries unchanged", async () => {
  const oauth = {
    type: "oauth",
    refresh: "r",
    access: "a",
    expires: 123,
    accountId: "acct",
    enterpriseUrl: "https://example.com",
  } as const
  await Bun.write(
    filepath,
    JSON.stringify(
      {
        openai: oauth,
      },
      null,
      2,
    ),
  )

  const result = await Auth.all()
  expect(result.openai).toEqual(oauth)
  const after = await Bun.file(filepath).json()
  expect(after.openai).toEqual(oauth)
})

test("all does not rewrite clean auth file", async () => {
  const clean = {
    openrouter: {
      type: "api",
      key: "sk-clean",
    },
  }
  await Bun.write(filepath, JSON.stringify(clean, null, 2))
  const before = await Bun.file(filepath).text()
  await Auth.all()
  const after = await Bun.file(filepath).text()
  expect(after).toBe(before)
})
