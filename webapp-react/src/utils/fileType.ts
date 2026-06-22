export enum FileCategory {
  Text = 'text',
  Image = 'image',
  Archive = 'archive',
  Other = 'other',
}

export type CodeEditorLanguage = 'plain' | 'nginx' | 'html' | 'css' | 'javascript' | 'typescript' | 'json' | 'xml' | 'yaml' | 'toml' | 'properties' | 'shell' | 'python' | 'go' | 'rust' | 'java' | 'c' | 'cpp' | 'sql' | 'lua' | 'perl' | 'ruby' | 'dockerfile'

const EXTENSION_CATEGORY_MAP: Record<string, FileCategory> = {
  conf: FileCategory.Text,
  nginx: FileCategory.Text,
  cfg: FileCategory.Text,
  ini: FileCategory.Text,
  toml: FileCategory.Text,
  env: FileCategory.Text,
  properties: FileCategory.Text,
  html: FileCategory.Text,
  htm: FileCategory.Text,
  css: FileCategory.Text,
  js: FileCategory.Text,
  ts: FileCategory.Text,
  jsx: FileCategory.Text,
  tsx: FileCategory.Text,
  json: FileCategory.Text,
  xml: FileCategory.Text,
  yaml: FileCategory.Text,
  yml: FileCategory.Text,
  csv: FileCategory.Text,
  sh: FileCategory.Text,
  bash: FileCategory.Text,
  zsh: FileCategory.Text,
  py: FileCategory.Text,
  php: FileCategory.Text,
  go: FileCategory.Text,
  rs: FileCategory.Text,
  java: FileCategory.Text,
  c: FileCategory.Text,
  cpp: FileCategory.Text,
  h: FileCategory.Text,
  hpp: FileCategory.Text,
  sql: FileCategory.Text,
  lua: FileCategory.Text,
  pl: FileCategory.Text,
  rb: FileCategory.Text,
  txt: FileCategory.Text,
  md: FileCategory.Text,
  log: FileCategory.Text,
  pem: FileCategory.Text,
  crt: FileCategory.Text,
  key: FileCategory.Text,
  csr: FileCategory.Text,
  cer: FileCategory.Text,
  p7b: FileCategory.Text,
  makefile: FileCategory.Text,
  dockerfile: FileCategory.Text,
  png: FileCategory.Image,
  jpg: FileCategory.Image,
  jpeg: FileCategory.Image,
  gif: FileCategory.Image,
  svg: FileCategory.Image,
  webp: FileCategory.Image,
  ico: FileCategory.Image,
  bmp: FileCategory.Image,
  avif: FileCategory.Image,
  tiff: FileCategory.Image,
  tif: FileCategory.Image,
  zip: FileCategory.Archive,
  tar: FileCategory.Archive,
  gz: FileCategory.Archive,
  tgz: FileCategory.Archive,
  bz2: FileCategory.Archive,
  xz: FileCategory.Archive,
  rar: FileCategory.Archive,
  '7z': FileCategory.Archive,
}

const NO_EXT_TEXT_FILES = new Set([
  'makefile',
  'dockerfile',
  'vagrantfile',
  'gemfile',
  'rakefile',
  'procfile',
  'readme',
  'license',
  'changelog',
  '.gitignore',
  '.dockerignore',
  '.env',
  '.editorconfig',
  '.npmrc',
  '.yarnrc',
  '.babelrc',
  '.eslintrc',
  '.prettierrc',
  '.stylelintrc',
])

const EXTENSION_EDITOR_LANGUAGE_MAP: Record<string, CodeEditorLanguage> = {
  conf: 'nginx',
  nginx: 'nginx',
  html: 'html',
  htm: 'html',
  vue: 'html',
  svelte: 'html',
  php: 'html',
  css: 'css',
  js: 'javascript',
  jsx: 'javascript',
  ts: 'typescript',
  tsx: 'typescript',
  json: 'json',
  xml: 'xml',
  yaml: 'yaml',
  yml: 'yaml',
  toml: 'toml',
  ini: 'properties',
  cfg: 'properties',
  env: 'properties',
  properties: 'properties',
  sh: 'shell',
  bash: 'shell',
  zsh: 'shell',
  py: 'python',
  go: 'go',
  rs: 'rust',
  java: 'java',
  c: 'c',
  h: 'c',
  cpp: 'cpp',
  hpp: 'cpp',
  sql: 'sql',
  lua: 'lua',
  pl: 'perl',
  rb: 'ruby',
  dockerfile: 'dockerfile',
}

function getExtension(filename: string): string {
  const idx = filename.lastIndexOf('.')
  if (idx <= 0) return ''
  return filename.slice(idx + 1).toLowerCase()
}

export function getFileCategory(filename: string): FileCategory {
  const ext = getExtension(filename)
  if (ext && EXTENSION_CATEGORY_MAP[ext]) return EXTENSION_CATEGORY_MAP[ext]
  const lower = filename.toLowerCase()
  if (NO_EXT_TEXT_FILES.has(lower) || lower.startsWith('.env')) return FileCategory.Text
  return FileCategory.Other
}

export function isEditableText(filename: string): boolean {
  return getFileCategory(filename) === FileCategory.Text
}

export function isImageFile(filename: string): boolean {
  return getFileCategory(filename) === FileCategory.Image
}

export function isArchiveFile(filename: string): boolean {
  return getFileCategory(filename) === FileCategory.Archive || /\.(tar\.gz)$/i.test(filename)
}

export function getCodeEditorLanguage(filename: string): CodeEditorLanguage {
  const lower = filename.toLowerCase()
  const basename = lower.split('/').pop() || lower
  if (basename === 'dockerfile') return 'dockerfile'
  if (basename.endsWith('.dockerfile')) return 'dockerfile'
  if (basename.startsWith('.env')) return 'properties'

  const ext = getExtension(basename)
  return (ext && EXTENSION_EDITOR_LANGUAGE_MAP[ext]) || 'plain'
}
