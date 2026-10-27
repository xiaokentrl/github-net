import { useCallback, useEffect, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { Ping, RuntimeInfo } from '../bindings/github.com/xiaokentrl/github-net/internal/app/service'
import type { RuntimeInfo as RuntimeInfoDTO } from '../bindings/github.com/xiaokentrl/github-net/internal/app/models'

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

function App() {
  const [load, setLoad] = useState<LoadState>('loading')
  const [loadError, setLoadError] = useState('')
  const [info, setInfo] = useState<RuntimeInfoDTO | null>(null)
  const [lastSequence, setLastSequence] = useState(0)
  const [dropped, setDropped] = useState(0)
  const [ping, setPing] = useState<PingState>({ kind: 'idle', text: '未发起' })

  // 收到的最新序号存在 ref 里，订阅回调才不会读到旧值。
  const sequenceRef = useRef(0)
  // 只认最后一次请求的结果，重试和 StrictMode 重复挂载都不会串台。
  const requestRef = useRef(0)

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

  useEffect(() => {
    loadRuntime()

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
      unsubscribe()
    }
  }, [loadRuntime])

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
    </main>
  )
}

export default App
