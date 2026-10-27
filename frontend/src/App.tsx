import { useCallback, useEffect, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { GetItems, Ping, RuntimeInfo } from '../bindings/github.com/xiaokentrl/github-net/internal/app/service'
import { HostsStatus } from '../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { ItemsSnapshot, RuntimeInfo as RuntimeInfoDTO } from '../bindings/github.com/xiaokentrl/github-net/internal/app/models'

// 事件名必须和 Go 侧 internal/event 的常量一致，改一边就要改另一边。
const STATUS_EVENT = 'app:runtime-status'

type StatusPayload = {
  sequence: number
  state: string
  windowVisible: boolean
  emittedAt: string
}

type LoadState = 'loading' | 'ready' | 'error'

type PingState = { kind: 'idle' | 'pending' | 'ok' | 'failed'; text: string }

// 目录只从后端 GetItems 来；这里只留汇总数字和每分类条目数，前端不再保存第二份域名清单。
type CatalogSummary = {
  categoryCount: number
  itemCount: number
  hostnameCount: number
  statusCounts: Record<string, number>
  categories: { id: string; name: string; itemCount: number }[]
}

function summarize(snapshot: ItemsSnapshot): CatalogSummary {
  const hostnames = new Set<string>()
  const statusCounts: Record<string, number> = {}
  let itemCount = 0
  for (const state of snapshot.hosts) {
    statusCounts[state.status] = (statusCounts[state.status] ?? 0) + 1
  }
  for (const category of snapshot.categories) {
    itemCount += category.items.length
    for (const item of category.items) {
      for (const hostname of item.hostnames) hostnames.add(hostname)
    }
  }
  return {
    categoryCount: snapshot.categories.length,
    itemCount,
    hostnameCount: hostnames.size,
    statusCounts,
    categories: snapshot.categories.map((category) => ({
      id: category.id,
      name: category.name,
      itemCount: category.items.length,
    })),
  }
}

function App() {
  const [load, setLoad] = useState<LoadState>('loading')
  const [loadError, setLoadError] = useState('')
  const [info, setInfo] = useState<RuntimeInfoDTO | null>(null)
  const [lastSequence, setLastSequence] = useState(0)
  const [dropped, setDropped] = useState(0)
  const [ping, setPing] = useState<PingState>({ kind: 'idle', text: '未发起' })
  const [catalog, setCatalog] = useState<CatalogSummary | null>(null)
  const [catalogLoad, setCatalogLoad] = useState<LoadState>('loading')
  const [catalogError, setCatalogError] = useState('')

  // 收到的最新序号存在 ref 里，订阅回调才不会读到旧值。
  const sequenceRef = useRef(0)
  // 只认最后一次请求的结果，重试和 StrictMode 重复挂载都不会串台。
  const requestRef = useRef(0)
  const catalogRequestRef = useRef(0)

  const loadRuntime = useCallback(() => {
    const request = requestRef.current + 1
    requestRef.current = request
    setLoad('loading')
    RuntimeInfo()
      .then((result) => {
        if (requestRef.current !== request) return
        setInfo(result)
        setLoad('ready')
      })
      .catch((err: unknown) => {
        if (requestRef.current !== request) return
        setLoadError(String(err))
        setLoad('error')
      })
  }, [])

  const loadCatalog = useCallback(() => {
    const request = catalogRequestRef.current + 1
    catalogRequestRef.current = request
    setCatalogLoad('loading')
    GetItems()
      .then((snapshot) => {
        if (catalogRequestRef.current !== request) return
        setCatalog(summarize(snapshot))
        setCatalogLoad('ready')
      })
      .catch((err: unknown) => {
        if (catalogRequestRef.current !== request) return
        setCatalogError(String(err))
        setCatalogLoad('error')
      })
  }, [])

  useEffect(() => {
    loadRuntime()
    loadCatalog()

    const unsubscribe = Events.On(STATUS_EVENT, (event: Events.WailsEvent) => {
      const payload = event.data as StatusPayload | undefined
      if (!payload || typeof payload.sequence !== 'number') return
      if (payload.sequence <= sequenceRef.current) {
        setDropped((count) => count + 1)
        return
      }
      sequenceRef.current = payload.sequence
      setLastSequence(payload.sequence)
    })

    return () => {
      // 让仍在路上的响应作废，并退订事件；退订必须调用，否则回调会一直堆积。
      requestRef.current += 1
      catalogRequestRef.current += 1
      unsubscribe()
    }
  }, [loadRuntime, loadCatalog])

  const runPing = (token: string) => {
    setPing({ kind: 'pending', text: '调用中' })
    Ping(token)
      .then((echo) => setPing({ kind: 'ok', text: `后端返回：${echo}` }))
      .catch((err: unknown) => setPing({ kind: 'failed', text: `后端拒绝：${String(err)}` }))
  }

  return (
    <main className="app-shell">
      <section className="welcome-panel" aria-labelledby="app-title">
        <div className="app-mark" aria-hidden="true">GN</div>
        <h1 id="app-title">{info?.appName ?? 'GitHub Net'}</h1>

        {load === 'loading' && <p role="status">正在向后端读取运行环境…</p>}
        {load === 'error' && (
          <p role="alert">
            读取失败：{loadError}
            <button type="button" onClick={loadRuntime}>重试</button>
          </p>
        )}
        {load === 'ready' && info && (
          <dl className="info-list">
            <div><dt>版本</dt><dd>{info.version}</dd></div>
            <div><dt>系统</dt><dd>{info.os} / {info.arch}</dd></div>
            <div><dt>启动时间</dt><dd>{info.startedAt}</dd></div>
            <div><dt>后端已发序号</dt><dd>{info.statusSequence}</dd></div>
          </dl>
        )}

        <p role="status" aria-live="polite">
          已收到的最新事件序号：{lastSequence}；因重复或过期丢弃：{dropped}
        </p>

        <p className="actions">
          <button type="button" onClick={() => runPing('')} disabled={ping.kind === 'pending'}>空令牌（预期失败）</button>
          <button type="button" onClick={() => runPing('probe-ok')} disabled={ping.kind === 'pending'}>正常令牌</button>
        </p>
        <p role="status" aria-live="polite">{ping.text}</p>
      </section>

      <section className="catalog-panel" aria-labelledby="catalog-title">
        <h2 id="catalog-title">后端目录（只读）</h2>
        {catalogLoad === 'loading' && <p role="status">正在向后端读取产品目录…</p>}
        {catalogLoad === 'error' && (
          <p role="alert">
            目录读取失败：{catalogError}
            <button type="button" onClick={loadCatalog}>重试</button>
          </p>
        )}
        {catalogLoad === 'ready' && catalog && (
          <>
            <dl className="info-list">
              <div><dt>分类</dt><dd>{catalog.categoryCount}</dd></div>
              <div><dt>项目</dt><dd>{catalog.itemCount}</dd></div>
              <div><dt>去重后主机名</dt><dd>{catalog.hostnameCount}</dd></div>
            </dl>
            <p role="status">
              Hosts 状态：{catalog.statusCounts[HostsStatus.HostsUnknown] ?? 0} 项 unknown。
              unknown 是「后端还没读过系统 Hosts」，不是「未替换」，也不是检测结果。
            </p>
            <ul className="catalog-list">
              {catalog.categories.map((category) => (
                <li key={category.id}>
                  <span>{category.name}</span>
                  <b>{category.itemCount}</b>
                </li>
              ))}
            </ul>
          </>
        )}
      </section>
    </main>
  )
}

export default App
