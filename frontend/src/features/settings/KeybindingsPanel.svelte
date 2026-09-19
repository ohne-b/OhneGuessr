<script lang="ts">
  import KeybindingButton from './KeybindingButton.svelte';
  import InfoLink from '../../components/InfoLink.svelte';
  import IconButton from '../../components/IconButton.svelte';
  import { onMount } from 'svelte';
  import { CONTROL_ROWS, currentBindings, resetControls, setBinding } from './keybindings.js';

  let capturing = $state<string | null>(null);
  const bindings = $derived.by(currentBindings);

  const codeFor = (action: string) => bindings[action]?.[0] ?? null;

  function captureKey(event: KeyboardEvent) {
    if (!capturing) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    const action = capturing;
    capturing = null;
    if (event.code === 'Escape') return;
    setBinding(action, event.code === 'Backspace' || event.code === 'Delete' ? null : event.code);
  }

  onMount(() => {
    window.addEventListener('keydown', captureKey, true);
    return () => window.removeEventListener('keydown', captureKey, true);
  });
</script>

<div class="setting">
  <div class="key-list">
    {#each CONTROL_ROWS as row}
      <div class="key-row">
        <span class="key-row-name">{row.label}</span>
        {#if row.items}
          <div class="key-cap-group" role="group" aria-label={row.label}>
            {#each row.items as item}
              {@const code = codeFor(item.action)}
              <KeybindingButton
                compact
                label={item.label}
                {code}
                capturing={capturing === item.action}
                onactivate={() => {
                  capturing = capturing ? null : item.action;
                }}
              />
            {/each}
          </div>
        {:else if row.action}
          {@const code = codeFor(row.action)}
          <KeybindingButton
            label={row.label}
            {code}
            capturing={capturing === row.action}
            onactivate={() => {
              capturing = capturing ? null : row.action!;
            }}
          />
        {/if}
      </div>
    {/each}
  </div>
</div>

<IconButton
  icon="reset-icon"
  class="controls-reset"
  aria-label="Reset controls to defaults"
  title="Reset controls to defaults"
  onclick={() => {
    capturing = null;
    resetControls();
  }}
/>
<InfoLink
  class="controls-info-link"
  href="https://github.com/ohne-b/OhneGuessr#controls"
  label="Open the usage guide on GitHub"
/>

<style>
  .key-cap-group {
    display: flex;
    gap: 4px;
  }
  .key-list {
    display: flex;
    flex-direction: column;
  }
  .key-row {
    display: grid;
    grid-template-columns: 1fr auto;
    align-items: center;
    gap: 10px;
    padding: 6px 8px;
    margin-inline: -8px;
  }
  .key-row:nth-child(odd) {
    background: var(--launcher-surface);
    border-radius: 4px;
  }
  .key-row-name {
    font-size: 15px;
    font-weight: 700;
  }

  :global {
    .controls-info-link {
      position: absolute;
      right: 0;
      bottom: 0;
    }
    .controls-reset {
      position: absolute;
      left: 0;
      bottom: 0;
      width: 30px;
      height: 30px;
    }
  }
</style>
