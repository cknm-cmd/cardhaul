<script lang="ts">
  import { AppService } from '../bindings/github.com/cknm-cmd/cardhaul'
  import { live } from './lib/state.svelte'
  import { bytes, duration, when, shortName } from './lib/format'

  const s = $derived(live.s)
  const pct = $derived(s && s.bytesTotal > 0 ? Math.round((s.bytesDone * 100) / s.bytesTotal) : 0)
  const checkPct = $derived(s && s.filesTotal > 0 ? Math.round((s.filesDone * 100) / s.filesTotal) : 0)

  const badge = $derived.by(() => {
    if (!s) return { text: '', cls: 'idle' }
    if (s.paused) return { text: 'Paused', cls: 'idle' }
    switch (s.phase) {
      case 'scanning': return { text: 'Checking', cls: 'accent' }
      case 'uploading': return { text: 'Uploading', cls: 'accent' }
      case 'verifying': return { text: 'Verifying', cls: 'accent' }
      case 'done': return { text: 'Safe to clear', cls: 'ok' }
      case 'problem': return { text: 'Needs attention', cls: 'warn' }
      default: return { text: 'Idle', cls: 'idle' }
    }
  })

  let error = $state('')
  async function act(fn: () => Promise<unknown>) {
    error = ''
    try { await fn() } catch (e) { error = String(e) }
  }
</script>

<div class="panel">
  <div class="row head">
    <h1>CardHaul</h1>
    <span class="pill {badge.cls}">{badge.text}</span>
  </div>

  <div class="detail">
    {#if !s}
      <p class="muted">Starting…</p>
    {:else if !s.signedIn}
      <p>Sign in to Google Drive to start uploading.</p>
      <button class="primary" onclick={() => AppService.OpenSettings()}>Open settings</button>
    {:else if s.phase === 'uploading'}
      <div class="ellipsis">{s.device} → {s.dest}</div>
      <div class="bar"><div style="width:{pct}%"></div></div>
      <div class="row between muted small">
        <span>{bytes(s.bytesDone)} of {bytes(s.bytesTotal)} · {s.filesDone}/{s.filesTotal} files</span>
        <span>{s.eta >= 0 ? '~' + duration(s.eta) : ''}</span>
      </div>
      <div class="faint small ellipsis">
        {bytes(s.speed)}/s{#if s.current?.length} · {shortName(s.current[0].name)} {s.current[0].percent}%{/if}
      </div>
    {:else if s.phase === 'verifying'}
      <div>Checking files against Drive <span class="faint small">({s.verifyMode === 'size' ? 'size only' : 'checksum'})</span></div>
      <div class="bar"><div style="width:{checkPct}%"></div></div>
      <div class="row between muted small">
        <span>{s.filesDone} of {s.filesTotal} files checked</span>
        <span>{s.eta >= 0 ? 'about ' + duration(s.eta) + ' left' : duration(s.elapsed)}</span>
      </div>
      <div class="faint small">Uploads are done. Keep {s.device} connected.</div>
    {:else if s.phase === 'scanning'}
      <div>{s.device}</div>
      <div class="muted small">{s.message}</div>
    {:else if s.phase === 'done'}
      <div>{s.device}: upload verified</div>
      <div class="muted small">{s.message}</div>
    {:else if s.phase === 'problem'}
      <div>{s.device}</div>
      <div class="muted small">{s.message}</div>
      {#each (s.problemFiles ?? []).slice(0, 3) as f}
        <div class="faint small ellipsis">• {f}</div>
      {/each}
    {:else}
      <div class="muted">{s.paused ? 'Auto-upload is paused.' : 'Plug in a device to start an upload.'}</div>
      {#if s.cancelled?.length}
        <div class="small">{s.cancelled.join(', ')}: cancelled. It'll upload next time it's plugged in.</div>
      {/if}
      {#if s.last}
        <div class="faint small last">Last: {s.last.device} · {s.last.result} · {when(s.last.finished)}</div>
      {/if}
    {/if}
    {#if error}<div class="error">{error}</div>{/if}
  </div>

  {#if s?.signedIn}
    <div class="sep"></div>
    {#if s.phase === 'uploading'}
      <button class="item" onclick={() => act(AppService.Pause)}>Pause auto-upload</button>
      <button class="item" onclick={() => act(AppService.Cancel)}>Cancel this upload</button>
    {:else if s.phase === 'verifying'}
      <button class="item" onclick={() => act(AppService.Cancel)}>Cancel verification</button>
    {:else if s.phase === 'done'}
      <button class="item" onclick={() => act(AppService.OpenDrive)}>Open in Google Drive</button>
      <button class="item" onclick={() => act(() => AppService.Eject(s.device))}>Eject {s.device}</button>
    {:else if s.phase === 'problem'}
      <button class="item" onclick={() => act(AppService.CheckNow)}>Check again</button>
    {:else}
      {#if s.cancelled?.length}
        <button class="item strong" onclick={() => act(AppService.CheckNow)}>Start {s.cancelled.join(', ')} again</button>
      {:else}
        <button class="item" onclick={() => act(AppService.CheckNow)}>Check devices now</button>
      {/if}
      {#if s.paused}
        <button class="item" onclick={() => act(AppService.Resume)}>Resume auto-upload</button>
      {:else}
        <button class="item" onclick={() => act(AppService.Pause)}>Pause auto-upload</button>
      {/if}
    {/if}
  {/if}

  <div class="sep"></div>
  <button class="item" onclick={() => AppService.OpenSettings()}>Settings…</button>
  <button class="item" onclick={() => AppService.OpenHistory()}>History</button>
  <button class="item muted" onclick={() => AppService.Quit()}>Quit CardHaul</button>
</div>

<style>
  .panel { margin: 6px; background: var(--bg); border: 1px solid var(--line); border-radius: 12px; padding: 6px; }
  .head { justify-content: space-between; padding: 8px 10px 4px; }
  .detail { padding: 6px 10px 10px; display: flex; flex-direction: column; gap: 6px; }
  .between { justify-content: space-between; }
  .small { font-size: 12px; }
  .last { margin-top: 2px; }
  .sep { border-top: 1px solid var(--line); margin: 4px 0; }
  .item { display: block; width: 100%; text-align: left; border: none; background: none; padding: 6px 10px; border-radius: 6px; }
  .item:hover { background: var(--bg-soft); }
  .item.strong { color: var(--accent); font-weight: 500; }
</style>
