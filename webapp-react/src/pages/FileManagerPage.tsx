import { Button, Group, Loader, SimpleGrid, Stack, Text } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { getFilesRoots } from '@/api/files'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PageShell } from '@/components/common/PageShell'
import { PathCell } from '@/components/common/PathCell'
import { SectionCard } from '@/components/common/SectionCard'
import { FileManager } from '@/components/files/FileManager'

function normalizePath(path: string): string {
  const trimmed = path.trim().replace(/\\+/g, '/').replace(/\/+/g, '/')
  if (!trimmed) return ''
  const prefixed = trimmed.startsWith('/') ? trimmed : `/${trimmed}`
  return prefixed === '/' ? '/' : prefixed.replace(/\/+$/, '')
}

function isPathWithinRoot(path: string, root: string): boolean {
  if (root === '/') return path.startsWith('/')
  return path === root || path.startsWith(`${root}/`)
}

function findRootForPath(path: string, roots: string[]): string {
  const normalizedPath = normalizePath(path)
  return roots
    .map(normalizePath)
    .filter((root) => root && isPathWithinRoot(normalizedPath, root))
    .sort((a, b) => b.length - a.length)[0] || ''
}

export function FileManagerPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [selectedRoot, setSelectedRoot] = useState(searchParams.get('root') || '')
  const rootsQuery = useQuery({ queryKey: ['files', 'roots'], queryFn: getFilesRoots })
  const roots = rootsQuery.data?.roots || []
  const currentPath = searchParams.get('path') || selectedRoot

  function selectRoot(root: string) {
    setSelectedRoot(root)
    // 根目录和当前目录一起写入 URL，浏览器刷新或复制链接都能恢复文件管理器位置。
    setSearchParams({ root, path: root }, { replace: true })
  }

  function clearRoot() {
    setSelectedRoot('')
    setSearchParams({}, { replace: true })
  }

  const handlePathChange = useCallback((path: string) => {
    if (!selectedRoot) return
    const nextRoot = findRootForPath(path, roots) || selectedRoot
    if (nextRoot !== selectedRoot) setSelectedRoot(nextRoot)
    if (searchParams.get('root') === nextRoot && searchParams.get('path') === path) return
    setSearchParams({ root: nextRoot, path }, { replace: true })
  }, [roots, searchParams, selectedRoot, setSearchParams])

  return (
    <PageShell>
      {rootsQuery.isError ? <ErrorAlert error={rootsQuery.error} title="加载白名单目录失败" /> : null}

      {!selectedRoot ? (
        <SectionCard title="选择要管理的目录" description="全局文件管理只允许进入后端返回的白名单根目录，避免误操作系统敏感路径。">
          {rootsQuery.isLoading ? <Group justify="center" py="xl"><Loader size="sm" /><Text size="sm" c="dimmed">正在加载目录...</Text></Group> : null}
          {!rootsQuery.isLoading && roots.length === 0 ? <Text c="dimmed">暂无可管理目录，请检查 Agent allowed_roots 配置。</Text> : null}
          <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="md">
            {roots.map((root) => (
              <SectionCard key={root} role="button" tabIndex={0} className="fileRootCard" onClick={() => selectRoot(root)} onKeyDown={(event) => { if (event.key === 'Enter') selectRoot(root) }}>
                <Stack gap="xs">
                  <Text size="xs" fw={700} c="blue" tt="uppercase">Root</Text>
                  <PathCell value={root} maxWidth="100%" />
                </Stack>
              </SectionCard>
            ))}
          </SimpleGrid>
        </SectionCard>
      ) : (
        <SectionCard actions={<Button variant="light" onClick={clearRoot}>选择根目录</Button>} title="文件浏览器" description="支持浏览、新建、删除、重命名、上传、下载、权限、压缩。">
          <FileManager rootPath={selectedRoot} allowedRoots={roots} initialPath={currentPath} onPathChange={handlePathChange} />
        </SectionCard>
      )}
    </PageShell>
  )
}
