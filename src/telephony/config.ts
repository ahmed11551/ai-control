import { ConnectionNode } from '@voximplant/websdk'

const NODE_MAP: Record<string, ConnectionNode> = {
  NODE_1: ConnectionNode.NODE_1,
  NODE_2: ConnectionNode.NODE_2,
  NODE_3: ConnectionNode.NODE_3,
  NODE_4: ConnectionNode.NODE_4,
  NODE_5: ConnectionNode.NODE_5,
  NODE_6: ConnectionNode.NODE_6,
  NODE_7: ConnectionNode.NODE_7,
  NODE_8: ConnectionNode.NODE_8,
  NODE_9: ConnectionNode.NODE_9,
  NODE_10: ConnectionNode.NODE_10,
  NODE_11: ConnectionNode.NODE_11,
  NODE_12: ConnectionNode.NODE_12,
  NODE_13: ConnectionNode.NODE_13,
}

export type TelephonyProvider = 'vox' | 'exolve'

/** Master switch: softphone (Vox + Exolve) off. Set true to turn telephony back on. */
export const TELEPHONY_ENABLED = true

/** Temporary kill-switch: Exolve softphone registration disabled. Set true to re-enable. */
export const EXOLVE_ENABLED = false

/**
 * Скорозвон временно приостановлен (код сохранён).
 * Включить снова: true (и на бэке AUTODIAL_ENABLED=1).
 */
export const AUTODIAL_ENABLED = false

export type VoxConfig = {
  node: ConnectionNode
  nodeLabel: string
  username: string
  password: string
  defaultDest: string
  configured: boolean
}

export type ExolveConfig = {
  sipLogin: string
  sipPassword: string
  defaultDest: string
  configured: boolean
}

export type TelephonyConfig = {
  provider: TelephonyProvider
  vox: VoxConfig
  exolve: ExolveConfig
}

const PROVIDER_KEY = 'forgecrm-telephony-provider-v2'

export function loadProviderPreference(fallback: TelephonyProvider): TelephonyProvider {
  try {
    const raw = localStorage.getItem(PROVIDER_KEY)
    if (raw === 'exolve' && !EXOLVE_ENABLED) return 'vox'
    if (raw === 'vox' || raw === 'exolve') return raw
  } catch {
    /* ignore */
  }
  return fallback === 'exolve' && !EXOLVE_ENABLED ? 'vox' : fallback
}

export function saveProviderPreference(provider: TelephonyProvider) {
  try {
    localStorage.setItem(PROVIDER_KEY, EXOLVE_ENABLED ? provider : provider === 'exolve' ? 'vox' : provider)
  } catch {
    /* ignore */
  }
}

function envProvider(): TelephonyProvider {
  const raw = String(import.meta.env.VITE_TELEPHONY_PROVIDER || '')
    .trim()
    .toLowerCase()
  if (raw === 'exolve' && !EXOLVE_ENABLED) return 'vox'
  return raw === 'exolve' ? 'exolve' : 'vox'
}

export function getTelephonyConfig(): TelephonyConfig {
  const nodeLabel = (import.meta.env.VITE_VOX_NODE || 'NODE_4').toUpperCase()
  const voxUsername = String(import.meta.env.VITE_VOX_USERNAME || '').trim()
  const voxPassword = String(import.meta.env.VITE_VOX_PASSWORD || '').trim()
  const voxDefaultDest = String(import.meta.env.VITE_VOX_DEFAULT_DEST || '').trim()
  const node = NODE_MAP[nodeLabel] ?? ConnectionNode.NODE_4

  const sipLogin = String(import.meta.env.VITE_EXOLVE_SIP_LOGIN || '').trim()
  const sipPassword = String(import.meta.env.VITE_EXOLVE_SIP_PASSWORD || '').trim()
  const exolveDefaultDest = String(import.meta.env.VITE_EXOLVE_DEFAULT_DEST || '').trim()

  return {
    provider: envProvider(),
    vox: {
      node,
      nodeLabel,
      username: voxUsername,
      password: voxPassword,
      defaultDest: voxDefaultDest,
      configured: TELEPHONY_ENABLED,
    },
    exolve: {
      sipLogin,
      sipPassword,
      defaultDest: exolveDefaultDest,
      // Session API may supply personal SIP; env is fallback only.
      configured: TELEPHONY_ENABLED && EXOLVE_ENABLED,
    },
  }
}
