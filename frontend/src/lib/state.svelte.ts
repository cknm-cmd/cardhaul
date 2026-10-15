import { Events } from '@wailsio/runtime'
import { AppService } from '../../bindings/github.com/cknm-cmd/cardhaul'
import type { Snapshot } from '../../bindings/github.com/cknm-cmd/cardhaul/internal/engine/models'

// Live engine state, shared by every view. Updated by the "state" event from Go.
export const live = $state<{ s: Snapshot | null }>({ s: null })

export function startLive() {
  AppService.State().then((s) => (live.s = s))
  Events.On('state', (e) => (live.s = e.data))
}
