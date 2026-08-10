import { App as AntApp } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import {
  endDeviceRemoteControl,
  getListDevicesQueryKey,
  heartbeatDeviceRemoteControl,
  useEndDeviceRemoteControl,
  useGetDeviceRemoteControl,
  useStartDeviceRemoteControl,
} from '../api/generated/device-farm'
import type { Device, RemoteControl } from '../api/generated/models'
import { unwrapData } from '../api/unwrap'

const storedDeviceKey = 'device-farm.remote-control-device'
const popupObservationWindowMs = 5_000
const remoteConnectTimeoutMs = 30_000

type RemoteDevice = Pick<Device, 'id' | 'serial'>

interface RemoteState {
  device: RemoteDevice
  popup: Window | null
  started: boolean
  opened: boolean
  startedAt: number
}

type RemoteAPIError = { code?: string; requestId?: string; message?: string; status?: number }

function isRemoteAlreadyGone(error: RemoteAPIError): boolean {
  return error.status === 404 || error.code === 'NOT_FOUND' || error.code === 'REMOTE_CONTROL_NOT_FOUND'
}

interface RemoteControlContextValue {
  device: RemoteDevice | null
  view?: RemoteControl
  start(device: RemoteDevice): void
  end(closePopup?: boolean, silent?: boolean): void
  reopen(): void
  isStarting: boolean
  isEnding: boolean
}

const RemoteControlContext = createContext<RemoteControlContextValue | null>(null)

function restoreDevice(): RemoteDevice | null {
  try {
    const stored = window.sessionStorage.getItem(storedDeviceKey)
    if (!stored) return null
    const parsed = JSON.parse(stored) as Partial<RemoteDevice>
    return typeof parsed.id === 'string' && typeof parsed.serial === 'string'
      ? { id: parsed.id, serial: parsed.serial }
      : null
  } catch {
    window.sessionStorage.removeItem(storedDeviceKey)
    return null
  }
}

export function RemoteControlProvider({
  children,
  connectTimeoutMs = remoteConnectTimeoutMs,
}: {
  children: ReactNode
  connectTimeoutMs?: number
}) {
  const { message } = AntApp.useApp()
  const queryClient = useQueryClient()
  const restored = useMemo(restoreDevice, [])
  const [remoteState, setRemoteState] = useState<RemoteState | null>(() => restored
    ? { device: restored, popup: null, started: true, opened: false, startedAt: Date.now() }
    : null)
  const remotePopup = useRef<Window | null>(null)
  const popupCloseDetectionArmed = useRef(false)
  const popupObservedOpenAt = useRef<number | null>(null)
  const endingRemote = useRef(false)
  const remoteAttempt = useRef(0)
  const startRemote = useStartDeviceRemoteControl()
  const endRemote = useEndDeviceRemoteControl()

  const remoteQuery = useGetDeviceRemoteControl(
    remoteState?.device.id ?? '',
    {
      query: {
        enabled: remoteState?.started === true,
        retry: false,
        refetchInterval: remoteState?.started ? 1_000 : false,
      },
    },
  )
  const remoteView = unwrapData<RemoteControl>(remoteQuery.data)

  const invalidate = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: getListDevicesQueryKey() })
    void queryClient.invalidateQueries({
      predicate: ({ queryKey }) => {
        const key = queryKey[0]
        return typeof key === 'string'
          && (key.startsWith('/api/v1/devices') || key.startsWith('/api/v1/device-reservations'))
      },
    })
  }, [queryClient])

  const clearRemote = useCallback((closePopup: boolean) => {
    const popup = remotePopup.current
    if (closePopup && popup) {
      try {
        popup.close()
      } catch {
        // A cross-origin browser context may already have severed the opener.
      }
    }
    remotePopup.current = null
    popupCloseDetectionArmed.current = false
    popupObservedOpenAt.current = null
    endingRemote.current = false
    remoteAttempt.current += 1
    window.sessionStorage.removeItem(storedDeviceKey)
    setRemoteState(null)
    invalidate()
  }, [invalidate])

  const finishRemote = useCallback((closePopup = true, silent = false, settleOnError = false) => {
    if (!remoteState || endingRemote.current) return
    endingRemote.current = true
    endRemote.mutate(
      { id: remoteState.device.id },
      {
        onSuccess: () => {
          if (!silent) message.success('远控已挂断，设备正在清理并重建')
          clearRemote(closePopup)
        },
        onError: (error) => {
          endingRemote.current = false
          const err = error as RemoteAPIError
          if (isRemoteAlreadyGone(err)) {
            if (!silent) message.info('远控会话已结束，设备状态已刷新')
            clearRemote(closePopup)
            return
          }
          if (settleOnError) {
            message.warning(`取消连接未能确认（${err.code ?? 'ERROR'}，request_id: ${err.requestId ?? '-'}），设备状态已刷新`)
            clearRemote(closePopup)
            return
          }
          message.error(`挂断失败（${err.code ?? 'ERROR'}，request_id: ${err.requestId ?? '-'}）：${err.message ?? ''}`)
        },
      },
    )
  }, [clearRemote, endRemote, message, remoteState])

  const navigatePopup = useCallback((popup: Window, url: string) => {
    popupCloseDetectionArmed.current = false
    popupObservedOpenAt.current = null
    popup.location.replace(url)
    setRemoteState((current) => current ? { ...current, opened: true } : current)
  }, [])

  const beginRemote = useCallback((device: RemoteDevice) => {
    if (remoteState) {
      message.warning('已有远控会话，请先挂断后再连接其他设备')
      return
    }
    const popup = window.open('about:blank', '_blank')
    if (!popup) {
      message.error('浏览器阻止了远控标签页，请允许本站弹出窗口后重试')
      return
    }
    popup.document.title = '正在连接设备…'
    popup.document.body.textContent = '正在预约设备并连接 STF，请稍候…'
    remotePopup.current = popup
    popupCloseDetectionArmed.current = false
    popupObservedOpenAt.current = null
    endingRemote.current = false
    const attempt = ++remoteAttempt.current
    setRemoteState({ device, popup, started: false, opened: false, startedAt: Date.now() })
    startRemote.mutate(
      { id: device.id },
      {
        onSuccess: (data) => {
          if (attempt !== remoteAttempt.current || endingRemote.current) {
            void endDeviceRemoteControl(device.id).catch(() => undefined).finally(invalidate)
            return
          }
          const view = unwrapData<RemoteControl>(data)
          window.sessionStorage.setItem(storedDeviceKey, JSON.stringify(device))
          setRemoteState((current) => current?.device.id === device.id ? { ...current, started: true } : current)
          if (view?.url) navigatePopup(popup, view.url)
          message.info('设备已预约，正在建立远控连接…')
          invalidate()
        },
        onError: (error) => {
          popup.close()
          clearRemote(false)
          // The server may have committed the reservation before the response
          // timed out or the browser lost it. DELETE is owner-scoped and idempotent.
          void endDeviceRemoteControl(device.id).catch(() => undefined).finally(invalidate)
          const err = error as RemoteAPIError
          if (isRemoteAlreadyGone(err)) {
            message.info('设备或远控会话已不存在，列表状态已刷新')
          } else {
            message.error(`远控连接失败（${err.code ?? 'ERROR'}，request_id: ${err.requestId ?? '-'}）：${err.message ?? ''}`)
          }
        },
      },
    )
  }, [clearRemote, invalidate, message, navigatePopup, remoteState, startRemote])

  useEffect(() => {
    if (!remoteState || remoteView?.status === 'connected' || endingRemote.current) return
    const remaining = connectTimeoutMs - (Date.now() - remoteState.startedAt)
    const timeout = window.setTimeout(() => {
      message.error('设备连接超时，已取消本次连接并刷新设备状态')
      finishRemote(true, true, true)
    }, Math.max(0, remaining))
    return () => window.clearTimeout(timeout)
  }, [connectTimeoutMs, finishRemote, message, remoteState, remoteView?.status])

  const reopenRemote = useCallback(() => {
    if (!remoteState || !remoteView?.url) {
      message.info('远控入口仍在准备，请稍后重试')
      return
    }
    const popup = window.open('about:blank', '_blank')
    if (!popup) {
      message.error('浏览器阻止了远控标签页，请允许本站弹出窗口后重试')
      return
    }
    remotePopup.current = popup
    setRemoteState((current) => current ? { ...current, popup, opened: false } : current)
    navigatePopup(popup, remoteView.url)
  }, [message, navigatePopup, remoteState, remoteView?.url])

  useEffect(() => {
    if (!remoteView?.url || remoteState?.opened || !remoteState?.popup || remoteState.popup.closed) return
    navigatePopup(remoteState.popup, remoteView.url)
    message.success('远控已连接；点击挂断会释放并清理设备')
  }, [message, navigatePopup, remoteState, remoteView?.url])

  useEffect(() => {
    if (!remoteState?.started || !remoteState.opened || !remoteState.popup) return
    const inspectPopup = () => {
      const popup = remotePopup.current
      if (!popup) return
      let closed = false
      try {
        closed = popup.closed
      } catch {
        return
      }
      if (!closed) {
        // Some browsers sever a cross-origin opener and immediately expose the
        // live STF tab as `closed`. Only trust a later close after observing a
        // stable post-navigation handle beyond the STF redirect window.
        popupObservedOpenAt.current ??= Date.now()
        if (Date.now() - popupObservedOpenAt.current >= popupObservationWindowMs) {
          popupCloseDetectionArmed.current = true
        }
        return
      }
      if (popupCloseDetectionArmed.current && document.visibilityState === 'visible') {
        finishRemote(false, true)
      }
    }
    const timer = window.setInterval(inspectPopup, 1_000)
    window.addEventListener('focus', inspectPopup)
    document.addEventListener('visibilitychange', inspectPopup)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('focus', inspectPopup)
      document.removeEventListener('visibilitychange', inspectPopup)
    }
  }, [finishRemote, remoteState])

  const sendHeartbeat = useCallback(async () => {
    if (!remoteState?.started || remoteView?.status !== 'connected') return
    try {
      const data = await heartbeatDeviceRemoteControl(remoteState.device.id)
      const next = unwrapData<RemoteControl>(data)
      if (next?.status === 'ended') {
        message.info('STF 已结束远控，设备正在清理并重建')
        clearRemote(true)
      }
    } catch {
      // A sustained failure stops renewal and the Reservation Reaper safely recovers the device.
    }
  }, [clearRemote, message, remoteState, remoteView?.status])

  useEffect(() => {
    if (!remoteState?.started || remoteView?.status !== 'connected') return
    const interval = Math.max(5, remoteView.heartbeat_interval_seconds) * 1_000
    let inFlight = false
    const heartbeat = () => {
      if (inFlight) return
      inFlight = true
      void sendHeartbeat().finally(() => { inFlight = false })
    }
    const timer = window.setInterval(heartbeat, interval)
    const resumeHeartbeat = () => heartbeat()
    window.addEventListener('focus', resumeHeartbeat)
    window.addEventListener('online', resumeHeartbeat)
    document.addEventListener('visibilitychange', resumeHeartbeat)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('focus', resumeHeartbeat)
      window.removeEventListener('online', resumeHeartbeat)
      document.removeEventListener('visibilitychange', resumeHeartbeat)
    }
  }, [remoteState?.started, remoteView?.heartbeat_interval_seconds, remoteView?.status, sendHeartbeat])

  useEffect(() => {
    if (!remoteQuery.isError) return
    const err = remoteQuery.error as RemoteAPIError
    if (isRemoteAlreadyGone(err)) {
      message.info('远控会话已结束，设备状态已刷新')
      clearRemote(false)
    }
  }, [clearRemote, message, remoteQuery.error, remoteQuery.isError])

  const value = useMemo<RemoteControlContextValue>(() => ({
    device: remoteState?.device ?? null,
    view: remoteView,
    start: beginRemote,
    end: finishRemote,
    reopen: reopenRemote,
    isStarting: startRemote.isPending,
    isEnding: endRemote.isPending,
  }), [beginRemote, endRemote.isPending, finishRemote, remoteState?.device, remoteView, reopenRemote, startRemote.isPending])

  return <RemoteControlContext.Provider value={value}>{children}</RemoteControlContext.Provider>
}

export function useRemoteControl(): RemoteControlContextValue {
  const value = useContext(RemoteControlContext)
  if (!value) throw new Error('useRemoteControl must be used within RemoteControlProvider')
  return value
}
