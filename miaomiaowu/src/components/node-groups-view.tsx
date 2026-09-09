import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

interface NodeSource {
  node_id: number
  subscription_id: number
  name: string
  node_name: string
  original_name: string
  last_sync_at?: string
}
export interface NodeGroup {
  id: number
  name: string
  protocol: string
  server: string
  port: number | string
  enabled: boolean
  tags: string[]
  sources: NodeSource[]
  member_ids: number[]
}

export function NodeGroupsView({
  onManage,
}: {
  onManage: (ids?: number[]) => void
}) {
  const [search, setSearch] = useState('')
  const [tag, setTag] = useState('all')
  const [page, setPage] = useState(1)
  const { data, isPending, error, refetch, isFetching } = useQuery({
    queryKey: ['nodes', 'grouped'],
    queryFn: async () =>
      (await api.get('/api/admin/nodes?view=grouped')).data as {
        groups: NodeGroup[]
      },
  })
  const groups = data?.groups ?? []
  const tags = [...new Set(groups.flatMap((g) => g.tags))].sort()
  const filtered = groups.filter(
    (g) =>
      (tag === 'all' || g.tags.includes(tag)) &&
      [
        g.name,
        g.server,
        ...g.tags,
        ...g.sources.flatMap((s) => [s.name, s.node_name, s.original_name]),
      ].some((value) => value.toLowerCase().includes(search.toLowerCase()))
  )
  const pages = Math.max(1, Math.ceil(filtered.length / 50))
  const currentPage = Math.min(page, pages)
  const shown = filtered.slice((currentPage - 1) * 50, currentPage * 50)
  const sourceCount = groups.reduce((n, g) => n + g.member_ids.length, 0)

  return (
    <div className='bg-background min-h-svh'>
      <main className='mx-auto w-full max-w-7xl space-y-5 px-4 py-8 pt-24 sm:px-6'>
        <div className='flex flex-wrap items-start justify-between gap-3'>
          <div>
            <h1 className='text-3xl font-semibold tracking-tight'>节点管理</h1>
            <p className='text-muted-foreground mt-2'>
              相同连接只显示一次，保留全部来源标签。各来源仍独立同步。
            </p>
          </div>
          <div className='flex flex-wrap gap-2'>
            <Button
              variant='outline'
              disabled={isFetching}
              onClick={() => void refetch()}
            >
              刷新列表
            </Button>
            <Button onClick={() => onManage()}>导入与来源管理</Button>
          </div>
        </div>
        <div className='bg-card rounded-xl border p-4'>
          <p className='font-medium'>
            聚合节点 {groups.length} · 来源记录 {sourceCount} · 合并显示{' '}
            {sourceCount - groups.length} 条重复记录
          </p>
          <div className='mt-3 flex flex-col gap-3 sm:flex-row'>
            <Input
              aria-label='搜索聚合节点'
              placeholder='搜索节点、服务器或来源'
              value={search}
              onChange={(e) => {
                setSearch(e.target.value)
                setPage(1)
              }}
            />
            <select
              aria-label='按来源标签筛选'
              className='border-input bg-background h-10 w-full rounded-md border px-3 sm:w-64'
              value={tag}
              onChange={(e) => {
                setTag(e.target.value)
                setPage(1)
              }}
            >
              <option value='all'>全部标签</option>
              {tags.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </div>
        </div>
        {isPending && <p role='status'>正在加载节点…</p>}
        {error && <p role='alert'>加载失败，请刷新重试。</p>}
        {!isPending && !error && filtered.length === 0 && (
          <p>没有符合条件的节点。</p>
        )}
        <div className='space-y-3'>
          {shown.map((g) => (
            <article
              key={g.id}
              className='bg-card min-w-0 rounded-xl border p-4'
              data-node-group={g.id}
            >
              <div className='flex flex-wrap items-start justify-between gap-3'>
                <div className='min-w-0 flex-1'>
                  <h2 className='font-semibold break-words'>{g.name}</h2>
                  <p className='text-muted-foreground mt-1 text-sm break-all'>
                    {g.protocol.toUpperCase()} · {g.server}:{g.port} ·{' '}
                    {g.enabled ? '已启用' : '已禁用'}
                  </p>
                  <div className='mt-2 flex flex-wrap gap-1.5'>
                    {g.tags.map((t) => (
                      <button
                        key={t}
                        className='bg-secondary text-secondary-foreground rounded-md px-2 py-1 text-xs'
                        onClick={() => {
                          setTag(t)
                          setPage(1)
                        }}
                      >
                        {t}
                      </button>
                    ))}
                  </div>
                </div>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => onManage(g.member_ids)}
                >
                  管理来源{g.sources.length > 0 ? ` (${g.sources.length})` : ''}
                </Button>
              </div>
              {g.sources.length > 0 && (
                <details className='mt-3 border-t pt-2 text-sm'>
                  <summary className='text-muted-foreground cursor-pointer py-1'>
                    查看来源名称与同步时间
                  </summary>
                  <ul className='mt-2 space-y-2'>
                    {g.sources.map((s) => (
                      <li
                        key={s.node_id}
                        className='bg-muted/40 flex flex-wrap items-center justify-between gap-2 rounded-md p-2'
                      >
                        <div className='min-w-0 flex-1 break-words'>
                          <strong>{s.name}</strong> ·{' '}
                          {s.original_name || s.node_name}
                          <p className='text-muted-foreground text-xs'>
                            最近同步：
                            {s.last_sync_at
                              ? new Date(s.last_sync_at).toLocaleString()
                              : '尚未同步'}
                          </p>
                        </div>
                        <Button
                          variant='ghost'
                          size='sm'
                          onClick={() => onManage([s.node_id])}
                        >
                          编辑此来源
                        </Button>
                      </li>
                    ))}
                  </ul>
                </details>
              )}
            </article>
          ))}
        </div>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <span className='text-muted-foreground text-sm'>
            {filtered.length} 个节点 · 第 {currentPage} / {pages} 页
          </span>
          <div className='flex gap-2'>
            <Button
              variant='outline'
              disabled={currentPage === 1}
              onClick={() => setPage(currentPage - 1)}
            >
              上一页
            </Button>
            <Button
              variant='outline'
              disabled={currentPage === pages}
              onClick={() => setPage(currentPage + 1)}
            >
              下一页
            </Button>
          </div>
        </div>
      </main>
    </div>
  )
}
