<script lang="ts">
  import { onMount } from 'svelte'
  import { AppService } from '../bindings/github.com/cknm-cmd/cardhaul'
  import type { Settings } from '../bindings/github.com/cknm-cmd/cardhaul/internal/config/models'
  import type { Volume } from '../bindings/github.com/cknm-cmd/cardhaul/internal/volumes/models'
  import type { Account } from '../bindings/github.com/cknm-cmd/cardhaul/internal/gauth/models'
  import { live } from './lib/state.svelte'
  import { bytes, duration } from './lib/format'

  let settings = $state<Settings | null>(null)
  let account = $state<Account | null>(null)
  let signingIn = $state(false)
  let error = $state('')

  let adding = $state(false)
  let available = $state<Volume[]>([])
  let chosen = $state('')
  let folder = $state('DCIM')

  let lastBytes = $state(0)
  let estimate = $state<Record<string, number>>({})

  const signedIn = $derived(live.s?.signedIn ?? false)
  const connected = $derived(new Set(live.s?.connected ?? []))

  async function load() {
    settings = await AppService.Settings()
    const history = (await AppService.History()) ?? []
    lastBytes = history.find((r) => r.bytes > 0)?.bytes ?? 100e9
    estimate = (await AppService.VerifyEstimate(lastBytes)) as Record<string, number>
    if (signedIn) loadAccount()
  }
  async function loadAccount() {
    try { account = await AppService.Account() } catch { account = null }
  }
  onMount(load)
  $effect(() => { if (signedIn && !account) loadAccount() })

  async function signIn() {
    error = ''
    signingIn = true
    try { await AppService.SignIn(); await loadAccount() } catch (e) { error = String(e) }
    signingIn = false
  }
  async function signOut() { await AppService.SignOut(); account = null }

  async function startAdd() {
    available = (await AppService.AvailableVolumes()) ?? []
    chosen = available[0]?.label ?? ''
    if (chosen) folder = await AppService.SuggestFolder(chosen)
    adding = true
  }
  async function pick(label: string) {
    chosen = label
    folder = await AppService.SuggestFolder(label)
  }
  async function addDevice() {
    error = ''
    try { await AppService.AddDevice(chosen, folder); adding = false; await load() } catch (e) { error = String(e) }
  }
  async function removeDevice(label: string) { await AppService.RemoveDevice(label); await load() }
  async function setVerify(mode: string) { await AppService.SetVerify(mode); await load() }
  async function setLogin(e: Event) {
    try { await AppService.SetStartAtLogin((e.target as HTMLInputElement).checked) } catch (err) { error = String(err) }
    await load()
  }
</script>

<main>
  <h1>Settings</h1>

  <h2>Google Drive</h2>
  <div class="card row">
    <div class="grow">
      {#if signedIn}
        <div>Connected{account ? ` as ${account.email}` : ''}</div>
        <div class="muted small">
          {account && account.freeBytes >= 0 ? `${bytes(account.freeBytes)} free · ` : ''}Uploads go to the CardHaul folder in your Drive
        </div>
      {:else}
        <div>Not connected</div>
        <div class="muted small">CardHaul can only see the files it uploads, nothing else in your Drive.</div>
      {/if}
    </div>
    {#if signedIn}
      <button onclick={signOut}>Sign out</button>
    {:else}
      <button class="primary" onclick={signIn} disabled={signingIn}>{signingIn ? 'Waiting for browser…' : 'Sign in with Google'}</button>
    {/if}
  </div>

  <h2>Devices</h2>
  {#each settings?.devices ?? [] as d (d.label)}
    <div class="card row dev">
      <div class="grow">
        <div>{d.label}</div>
        <div class="muted small ellipsis">{d.folder === '.' ? 'Whole drive' : d.folder} → CardHaul/{d.dest}</div>
      </div>
      <span class="pill {connected.has(d.label) ? 'ok' : 'idle'}">{connected.has(d.label) ? 'Connected' : 'Not connected'}</span>
      <button class="link" onclick={() => removeDevice(d.label)}>Remove</button>
    </div>
  {:else}
    <p class="muted small">No devices yet. Plug in your camera or card, then add it.</p>
  {/each}

  {#if adding}
    <div class="card add">
      {#if available.length === 0}
        <div class="muted">No new drives found. Plug in your camera or card reader, then try again.</div>
        <div class="row actions"><button onclick={startAdd}>Look again</button><button class="link" onclick={() => (adding = false)}>Cancel</button></div>
      {:else}
        <div class="muted small">Choose the drive</div>
        {#each available as v (v.label)}
          <label class="row choice"><input type="radio" name="vol" checked={chosen === v.label} onchange={() => pick(v.label)} />
            {v.label} <span class="faint small">{bytes(v.totalBytes)}</span></label>
        {/each}
        <label class="row folder">Folder to upload
          <select bind:value={folder}>
            <option value="DCIM">DCIM (camera footage)</option>
            <option value=".">Everything on the drive</option>
          </select>
        </label>
        <div class="row actions"><button class="primary" onclick={addDevice}>Add {chosen}</button><button class="link" onclick={() => (adding = false)}>Cancel</button></div>
      {/if}
    </div>
  {:else}
    <button onclick={startAdd}>+ Add device</button>
  {/if}

  <h2>After uploading, check files by</h2>
  <label class="card opt" class:on={settings?.verify === 'checksum'}>
    <input type="radio" name="verify" checked={settings?.verify === 'checksum'} onchange={() => setVerify('checksum')} />
    <div>
      <div>Checksum <span class="pill accent">Recommended</span></div>
      <div class="muted small">Safest. Reads every file back from the device and compares it with Google's copy, so it catches files that are damaged inside. Takes longer: about 1 hour per 100 GB.</div>
    </div>
  </label>
  <label class="card opt" class:on={settings?.verify === 'size'}>
    <input type="radio" name="verify" checked={settings?.verify === 'size'} onchange={() => setVerify('size')} />
    <div>
      <div>File size only</div>
      <div class="muted small">Fast: done in seconds. Catches missing or incomplete files, but not a file that's the right size and damaged inside.</div>
    </div>
  </label>
  {#if settings}
    <p class="faint small">For an upload of {bytes(lastBytes)}, checking would take
      {settings.verify === 'checksum' ? 'about ' + duration(estimate.checksum ?? -1) : 'a few seconds'}.</p>
  {/if}

  <h2>Options</h2>
  <label class="row between"><span>Start CardHaul when you log in</span>
    <input type="checkbox" checked={settings?.startAtLogin} onchange={setLogin} /></label>

  {#if error}<div class="error">{error}</div>{/if}
</main>

<style>
  main { padding: 20px 24px 28px; }
  .grow { flex: 1; min-width: 0; }
  .small { font-size: 12px; }
  .between { justify-content: space-between; }
  .dev { margin-bottom: 6px; }
  .add { display: flex; flex-direction: column; gap: 8px; }
  .choice { gap: 8px; }
  .folder { gap: 8px; justify-content: space-between; }
  .actions { gap: 12px; margin-top: 4px; }
  .opt { display: flex; gap: 10px; align-items: flex-start; margin-bottom: 8px; cursor: pointer; }
  .opt.on { border: 2px solid var(--accent); padding: 9px 11px; }
  .opt input { margin-top: 3px; }
  select { font: inherit; padding: 3px 6px; border-radius: 6px; border: 1px solid var(--line); background: var(--bg); color: var(--fg); }
</style>
