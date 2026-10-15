<script lang="ts">
  import { onMount } from 'svelte'
  import { startLive } from './lib/state.svelte'
  import Tray from './Tray.svelte'
  import Settings from './Settings.svelte'
  import History from './History.svelte'

  // Each native window loads the same page with a different hash: #/tray, #/settings, #/history.
  let route = $state(location.hash.replace(/^#\/?/, '') || 'settings')

  onMount(() => {
    startLive()
    if (route === 'tray') document.body.classList.add('tray')
    window.addEventListener('hashchange', () => (route = location.hash.replace(/^#\/?/, '')))
  })
</script>

{#if route === 'tray'}
  <Tray />
{:else if route === 'history'}
  <History />
{:else}
  <Settings />
{/if}
