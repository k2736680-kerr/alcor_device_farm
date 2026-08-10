import { readdir, readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const generatedRoot = new URL('../src/api/generated/', import.meta.url)

async function trimGenerated(directory) {
  const entries = await readdir(directory, { withFileTypes: true })
  await Promise.all(entries.map(async (entry) => {
    const path = join(fileURLToPath(directory), entry.name)
    if (entry.isDirectory()) {
      await trimGenerated(new URL(`${entry.name}/`, directory))
      return
    }
    if (!entry.name.endsWith('.ts')) return
    const source = await readFile(path, 'utf8')
    const formatted = source.replace(/[ \t]+$/gm, '')
    if (formatted !== source) await writeFile(path, formatted, 'utf8')
  }))
}

await trimGenerated(generatedRoot)
