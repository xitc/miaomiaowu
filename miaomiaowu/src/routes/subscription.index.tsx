// @ts-nocheck
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { QRCodeCanvas } from 'qrcode.react'
import {
  Copy,
  Download,
  Monitor,
  Network,
  QrCode,
  Smartphone,
  ChevronDown,
} from 'lucide-react'
import { toast } from 'sonner'
import { Topbar } from '@/components/layout/topbar'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

// Import local icons
import clashIcon from '@/assets/icons/clash_color.png'
import stashIcon from '@/assets/icons/stash_color.png'
import shadowrocketIcon from '@/assets/icons/shadowrocket_color.png'
import surfboardIcon from '@/assets/icons/surfboard_color.png'
import surgeIcon from '@/assets/icons/surge_color.png'
import surgeMacIcon from '@/assets/icons/surgeformac_icon_color.png'
import loonIcon from '@/assets/icons/loon_color.png'
import quanxIcon from '@/assets/icons/quanx_color.png'
import egernIcon from '@/assets/icons/egern_color.png'
import singboxIcon from '@/assets/icons/sing-box_color.png'
import v2rayIcon from '@/assets/icons/v2ray_color.png'
import uriIcon from '@/assets/icons/uri-color.svg'

// @ts-ignore - retained simple route definition
export const Route = createFileRoute('/subscription/')({
  beforeLoad: () => {
    const token = useAuthStore.getState().auth.accessToken
    if (!token) {
      throw redirect({ to: '/' })
    }
  },
  component: SubscriptionPage,
})

type OutputMode = 'normal' | 'provider'

type SubscribeFile = {
  id: number
  name: string
  description: string
  type: string
  filename: string
  file_short_code?: string
  custom_short_code?: string
  raw_output?: boolean
  normal_link_enabled?: boolean
  provider_link_enabled?: boolean
  default_output_mode?: OutputMode
  expire_at?: string | null
  created_at: string
  updated_at: string
  latest_version?: number
}

const ICON_MAP: Record<string, any> = {
  clash: Smartphone,
  'openclash-redirhost': Network,
  'openclash-fakeip': Monitor,
}

// Client types configuration with icons and names
const CLIENT_TYPES = [
  { type: 'clash', name: 'Clash', icon: clashIcon },
  { type: 'stash', name: 'Stash', icon: stashIcon },
  { type: 'clash-to-shadowrocket', name: 'Shadowrocket', icon: shadowrocketIcon },
  { type: 'surfboard', name: 'Surfboard', icon: surfboardIcon },
  { type: 'surge', name: 'Surge', icon: surgeIcon },
  { type: 'surgemac', name: 'Surge Mac', icon: surgeMacIcon },
  { type: 'clash-to-surge', name: 'Clash→Surge', icon: surgeIcon },
  { type: 'loon', name: 'Loon', icon: loonIcon },
  { type: 'clash-to-loon', name: 'Clash→Loon', icon: loonIcon },
  { type: 'clash-to-loon-kelee', name: 'Clash→Loon(kelee)', icon: loonIcon },
  { type: 'qx', name: 'QuantumultX', icon: quanxIcon },
  { type: 'egern', name: 'Egern', icon: egernIcon },
  { type: 'sing-box', name: 'sing-box', icon: singboxIcon },
  { type: 'v2ray', name: 'V2Ray', icon: v2rayIcon },
  { type: 'uri', name: 'URI', icon: uriIcon },
] as const

function resolveFileModes(file: SubscribeFile) {
  const providerEnabled = !!file.provider_link_enabled
  // Legacy fallback: old API only had raw_output for provider-ish links
  const normalEnabled = file.normal_link_enabled ?? !providerEnabled
  let defaultMode: OutputMode = file.default_output_mode === 'provider' ? 'provider' : 'normal'
  if (defaultMode === 'provider' && !providerEnabled) defaultMode = 'normal'
  if (defaultMode === 'normal' && !normalEnabled && providerEnabled) defaultMode = 'provider'
  return { normalEnabled, providerEnabled, defaultMode, dual: normalEnabled && providerEnabled }
}

function SubscriptionPage() {
  const { auth } = useAuthStore()
  const [qrValue, setQrValue] = useState<string | null>(null)
  // 为每个订阅文件保存当前显示的URL
  const [displayURLs, setDisplayURLs] = useState<Record<number, string>>({})
  // 双模式订阅的当前展示模式
  const [modeByFile, setModeByFile] = useState<Record<number, OutputMode>>({})

  const { data: subscribeFilesData } = useQuery({
    queryKey: ['user-subscriptions'],
    queryFn: async () => {
      const response = await api.get('/api/subscriptions')
      return response.data as { subscriptions: SubscribeFile[]; user_short_code: string }
    },
    enabled: Boolean(auth.accessToken),
    staleTime: 60 * 1000,
  })

  const subscribeFiles = subscribeFilesData?.subscriptions ?? []
  const userShortCode = subscribeFilesData?.user_short_code ?? ''

  // 只有当订阅数据已加载且 userShortCode 为空时（短链接未启用）才需要获取 token
  const { data: tokenData } = useQuery({
    queryKey: ['user-token'],
    queryFn: async () => {
      const response = await api.get('/api/user/token')
      return response.data as { token: string }
    },
    enabled: Boolean(auth.accessToken) && subscribeFilesData !== undefined && !userShortCode,
    staleTime: 5 * 60 * 1000,
  })

  const userToken = tokenData?.token ?? ''

  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat('zh-CN', {
        dateStyle: 'medium',
        timeStyle: 'short',
        hour12: false,
      }),
    []
  )

  const baseURL =
    api.defaults.baseURL ??
    (typeof window !== 'undefined'
      ? `${window.location.protocol}//${window.location.host}`
      : 'http://localhost:8080')

  const buildSubscriptionURL = (
    filename: string,
    fileShortCode: string | undefined,
    customShortCode: string | undefined,
    opts?: { clientType?: string; mode?: OutputMode; dual?: boolean }
  ) => {
    const clientType = opts?.clientType
    const mode = opts?.mode
    const dual = opts?.dual
    // Use custom short code or file short code + user short code for composite short link
    const fileCode = customShortCode || fileShortCode
    if (fileCode && userShortCode) {
      const compositeCode = fileCode + userShortCode
      const url = new URL(`/${compositeCode}`, baseURL)
      if (clientType) {
        url.searchParams.set('t', clientType)
      }
      // 双模式或显式非默认时带上 mode，保证复制/导入行为明确
      if (mode && dual) {
        url.searchParams.set('mode', mode)
      }
      return url.toString()
    }

    // Otherwise, use full URL with filename and user token for authentication
    const url = new URL('/api/clash/subscribe', baseURL)
    url.searchParams.set('filename', filename)
    if (clientType) {
      url.searchParams.set('t', clientType)
    }
    if (mode && dual) {
      url.searchParams.set('mode', mode)
    }
    if (userToken) {
      url.searchParams.set('token', userToken)
    }
    return url.toString()
  }

  const handleCopy = async (fileId: number, urlText: string, clientName: string, clientType?: string) => {
    // 更新该订阅文件显示的URL
    setDisplayURLs(prev => ({ ...prev, [fileId]: urlText }))

    if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(urlText)
        toast.success(`${clientName} 订阅链接已复制`)
        if (clientType === 'clash-to-shadowrocket') {
          toast.info('请在 Shadowrocket 配置界面点击右上角 + 号，选择添加订阅链接', { duration: 5000 })
        }
        return
      } catch (_) {
        // fall through
      }
    }

    toast.error('复制失败(需要https)，请手动复制')
  }

  return (
    <div className='min-h-svh bg-background'>
      <Topbar />
      <main className='mx-auto w-full max-w-5xl px-4 py-8 sm:px-6 pt-24'>
        <section className='space-y-4 text-center sm:text-left'>
          <h1 className='text-3xl font-semibold tracking-tight'>订阅链接</h1>
          <p className='mt-2 text-sm font-semibold text-destructive'>转换客户端代理是从substore抄过来的, 没有完全测试，有BUG请联系开发者</p>
        </section>

        <section className='mt-8 grid gap-6 sm:grid-cols-1 md:grid-cols-2 lg:grid-cols-3'>
          {subscribeFiles.length === 0 ? (
            <Card className='sm:col-span-1 md:col-span-2 lg:col-span-3 border-dashed shadow-none w-full'>
              <CardHeader>
                <CardTitle>暂无可用订阅</CardTitle>
                <CardDescription>管理员尚未为您分配订阅链接，请联系管理员进行分配。</CardDescription>
              </CardHeader>
            </Card>
          ) : null}

          {subscribeFiles.map((file) => {
            const Icon = ICON_MAP[file.name] ?? QrCode
            const modes = resolveFileModes(file)
            const activeMode: OutputMode = modeByFile[file.id] ?? modes.defaultMode
            const isProviderView = activeMode === 'provider' && modes.providerEnabled
            const isRawOnly = !!file.raw_output && !modes.providerEnabled

            const subscribeURL = buildSubscriptionURL(file.filename, file.file_short_code, file.custom_short_code, {
              mode: activeMode,
              dual: modes.dual,
            })
            // 使用当前显示的URL，如果没有则使用默认URL
            const displayURL = displayURLs[file.id] || subscribeURL
            const clashURL = `clash://install-config?url=${encodeURIComponent(subscribeURL)}`
            const updatedLabel = file.updated_at
              ? dateFormatter.format(new Date(file.updated_at))
              : null

            return (
              <Card key={file.id} className='flex flex-col justify-between'>
                <CardHeader>
                  <div className='flex items-start gap-3 overflow-hidden'>
                    <button
                      onClick={() => setQrValue(displayURL)}
                      className='flex size-12 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary transition-all hover:bg-primary/20 hover:scale-110 active:scale-95 cursor-pointer'
                      title='点击显示二维码'
                    >
                      <Icon className='size-6' />
                    </button>
                    <div className='flex-1 min-w-0 space-y-1 text-left'>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <CardTitle className='text-lg truncate'>
                            {file.name}
                          </CardTitle>
                        </TooltipTrigger>
                        <TooltipContent>{file.name}</TooltipContent>
                      </Tooltip>
                      <CardDescription>{file.description || '—'}</CardDescription>
                    </div>
                  </div>
                </CardHeader>
                <CardContent className='space-y-4'>
                  <div className='flex items-center justify-end gap-2 flex-wrap'>
                    {file.expire_at ? (
                      new Date(file.expire_at) < new Date() ? (
                        <Badge variant='destructive'>已过期</Badge>
                      ) : (
                        <Badge variant='outline'>
                          到期: {dateFormatter.format(new Date(file.expire_at))}
                        </Badge>
                      )
                    ) : (
                      <Badge variant='outline' className='text-muted-foreground'>
                        永久有效
                      </Badge>
                    )}
                    {modes.providerEnabled && (
                      <Badge variant='outline' className='bg-sky-500/10 text-sky-700 dark:text-sky-400'>
                        Provider
                      </Badge>
                    )}
                    {updatedLabel ? (
                      <p className='text-xs text-muted-foreground'>
                        {updatedLabel}
                      </p>
                    ) : null}
                    {file.latest_version ? (
                      <Badge variant='secondary' className='text-xs'>
                        v{file.latest_version}
                      </Badge>
                    ) : null}
                  </div>

                  {modes.dual && (
                    <div className='inline-flex w-full rounded-md border text-xs'>
                      <button
                        type='button'
                        className={`flex-1 px-2 py-1.5 rounded-l-md ${activeMode === 'normal' ? 'bg-accent text-foreground' : 'text-muted-foreground'}`}
                        onClick={() => {
                          setModeByFile((prev) => ({ ...prev, [file.id]: 'normal' }))
                          const next = buildSubscriptionURL(file.filename, file.file_short_code, file.custom_short_code, {
                            mode: 'normal',
                            dual: true,
                          })
                          setDisplayURLs((prev) => ({ ...prev, [file.id]: next }))
                        }}
                      >
                        普通链接
                      </button>
                      <button
                        type='button'
                        className={`flex-1 px-2 py-1.5 rounded-r-md border-l ${activeMode === 'provider' ? 'bg-accent text-foreground' : 'text-muted-foreground'}`}
                        onClick={() => {
                          setModeByFile((prev) => ({ ...prev, [file.id]: 'provider' }))
                          const next = buildSubscriptionURL(file.filename, file.file_short_code, file.custom_short_code, {
                            mode: 'provider',
                            dual: true,
                          })
                          setDisplayURLs((prev) => ({ ...prev, [file.id]: next }))
                        }}
                      >
                        Provider 链接
                      </button>
                    </div>
                  )}

                  <div className='break-all rounded-lg border bg-muted/40 p-3 font-mono text-xs shadow-inner sm:text-sm'>
                    {displayURL}
                  </div>
                  <div className='grid grid-cols-2 gap-2'>
                    {isRawOnly || isProviderView ? (
                      <>
                        <Button
                          size='sm'
                          className='w-full col-span-2 transition-transform hover:-translate-y-0.5 hover:shadow-md active:translate-y-0.5 active:scale-95'
                          onClick={() => handleCopy(file.id, subscribeURL, isProviderView ? 'Provider 链接' : '订阅链接')}
                        >
                          <Copy className='mr-2 size-4' />
                          复制{isProviderView ? ' Provider' : ''}订阅链接
                        </Button>
                        {isProviderView && (
                          <Button
                            size='sm'
                            variant='secondary'
                            className='w-full col-span-2 transition-transform hover:-translate-y-0.5 hover:shadow-md active:translate-y-0.5 active:scale-95'
                            asChild
                          >
                            <a href={clashURL}>
                              <Download className='mr-2 size-4' />导入 Clash
                            </a>
                          </Button>
                        )}
                        {isProviderView && (
                          <p className='col-span-2 text-[11px] text-muted-foreground'>
                            Provider 链接仅提供 Mihomo/Clash YAML、二维码与 Clash 导入。
                          </p>
                        )}
                      </>
                    ) : (
                      <>
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button
                              size='sm'
                              className='w-full transition-transform hover:-translate-y-0.5 hover:shadow-md active:translate-y-0.5 active:scale-95'
                            >
                              <Copy className='mr-2 size-4' />
                              复制
                              <ChevronDown className='ml-2 size-4' />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align='end' className='w-56'>
                            {CLIENT_TYPES.map((client) => {
                              const clientURL = buildSubscriptionURL(
                                file.filename,
                                file.file_short_code,
                                file.custom_short_code,
                                { clientType: client.type, mode: activeMode, dual: modes.dual }
                              )
                              return (
                                <DropdownMenuItem
                                  key={client.type}
                                  onClick={() => handleCopy(file.id, clientURL, client.name, client.type)}
                                  className='cursor-pointer'
                                >
                                  <img src={client.icon} alt={client.name} className='mr-2 size-4' />
                                  {client.name}
                                </DropdownMenuItem>
                              )
                            })}
                          </DropdownMenuContent>
                        </DropdownMenu>
                        <Button
                          size='sm'
                          variant='secondary'
                          className='w-full transition-transform hover:-translate-y-0.5 hover:shadow-md active:translate-y-0.5 active:scale-95'
                          asChild
                        >
                          <a href={clashURL}>
                            <Download className='mr-2 size-4' />导入 Clash
                          </a>
                        </Button>
                      </>
                    )}
                  </div>
                </CardContent>
              </Card>
            )
          })}
        </section>
      </main>

      <Dialog
        open={Boolean(qrValue)}
        onOpenChange={(open) => {
          if (!open) {
            setQrValue(null)
          }
        }}
      >
        <DialogContent className='sm:max-w-sm'>
          <DialogHeader>
            <DialogTitle>订阅二维码</DialogTitle>
            <DialogDescription>使用手机扫描二维码快速导入订阅链接。</DialogDescription>
          </DialogHeader>
          {qrValue ? (
            <div className='flex flex-col items-center gap-4'>
              <div className='rounded-xl border bg-white p-4 shadow-inner'>
                <QRCodeCanvas value={qrValue} size={220} level='M' includeMargin />
              </div>
              <div className='font-mono text-xs break-all text-center text-muted-foreground'>
                {qrValue}
              </div>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  )
}
