import { readFileSync } from "node:fs";
import { join } from "node:path";

/**
 * Facts about the tools, read from the generated docs/tools.md at build time,
 * so the landing page's numbers and programme can't drift from the code.
 */
export type Level = "read" | "write" | "delete";

export interface Tool {
  name: string;
  title: string;
  level: Level;
}

export interface Toolset {
  name: string;
  summary: string;
  tools: Tool[];
}

const LABELS: Record<string, Level> = { "🟢 read": "read", "🟠 write": "write", "🔴 delete": "delete" };

// Builds run from website/, and the repo root is its parent.
const md = readFileSync(join(process.cwd(), "../docs/tools.md"), "utf8");

export const toolsets: Toolset[] = [];
for (const section of md.split(/^## /m).slice(1)) {
  const [heading, ...rest] = section.split("\n");
  const name = heading.trim();
  if (!/^[a-z]+$/.test(name)) continue;
  const body = rest.join("\n");
  const summary = body.trim().split("\n")[0].trim();
  const tools: Tool[] = [];
  for (const m of body.matchAll(/^\| \[`(emby_[a-z0-9_]+)`\]\([^)]*\) \| ([^|]+) \| ([^|]+) \|$/gm)) {
    tools.push({ name: m[1], level: LABELS[m[2].trim()] ?? "read", title: m[3].trim() });
  }
  toolsets.push({ name, summary, tools });
}

/** Prompt names, in the order docs/tools.md lists them. */
export const prompts: string[] = [...(md.split(/^## Prompts/m)[1] ?? "").matchAll(/^### (\S+)/gm)].map((m) => m[1]);

const count = (level?: Level) => toolsets.reduce((n, t) => n + t.tools.filter((x) => !level || x.level === level).length, 0);

export const totals = {
  tools: count(),
  read: count("read"),
  write: count("write"),
  delete: count("delete"),
  toolsets: toolsets.length,
  prompts: prompts.length,
};
