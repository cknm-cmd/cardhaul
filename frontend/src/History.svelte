<script lang="ts">
  import { onMount } from 'svelte'
  import { AppService } from '../bindings/github.com/cknm-cmd/cardhaul'
  import type { Run } from '../bindings/github.com/cknm-cmd/cardhaul/internal/config/models'
  import { live } from './lib/state.svelte'
  import { bytes, when } from './lib/format'

  let runs = $state<Run[]>([])
  const load = async () => (runs = (await AppService.History()) ?? [])
  onMount(load)
  // Refresh whenever a run finishes.
  $effect(() => { live.s?.last; load() })

  const cls: Record<string, string> = { verified: 'ok', failed: 'warn', error: 'warn', disconnected: 'warn' }
  const label: Record<string, string> = {
    verified: 'Verified', failed: 'Check failed', cancelled: 'Cancelled', paused: 'Paused',
    disconnected: 'Disconnected', error: 'Error',
  }
</script>

<main>
  <h1>History</h1>
  {#if runs.length === 0}
    <p class="muted">Your uploads will appear here.</p>
  {:else}
    <table>
      <thead><tr><th>When</th><th>Device</th><th>Uploaded</th><th>Result</th></tr></thead>
      <tbody>
        {#each runs as r}
          <tr title={r.message}>
            <td class="muted">{when(r.finished)}</td>
            <td>{r.device}</td>
            <td>{r.files ? `${r.files} files · ${bytes(r.bytes)}` : 'Nothing new'}</td>
            <td><span class="pill {cls[r.result] ?? 'idle'}">{label[r.result] ?? r.result}</span></td>
          </tr>
          {#if r.result !== 'verified' && r.message}
            <tr class="msg"><td></td><td colspan="3" class="faint">{r.message}</td></tr>
          {/if}
        {/each}
      </tbody>
    </table>
  {/if}
</main>

<style>
  main { padding: 20px 24px; }
  table { width: 100%; border-collapse: collapse; margin-top: 14px; }
  th { text-align: left; font-weight: 500; color: var(--muted); font-size: 12px; padding: 6px 8px; border-bottom: 1px solid var(--line); }
  td { padding: 8px; border-bottom: 1px solid var(--line); }
  tr.msg td { border-bottom: 1px solid var(--line); padding-top: 0; font-size: 12px; }
</style>
